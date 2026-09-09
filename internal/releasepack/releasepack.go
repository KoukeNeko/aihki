package releasepack

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type Target struct {
	OS   string
	Arch string
}

type Config struct {
	RepoRoot string
	Output   string
	Version  string
	Commit   string
	Epoch    int64
	Targets  []Target
	Signing  Signing
}

type Artifact struct {
	Name   string `json:"name"`
	OS     string `json:"os,omitempty"`
	Arch   string `json:"arch,omitempty"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

var versionPattern = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-.][0-9A-Za-z.-]+)?$`)
var commitPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
var targetPartPattern = regexp.MustCompile(`^[a-z0-9]+$`)

func DefaultTargets() []Target {
	return []Target{{OS: "darwin", Arch: "amd64"}, {OS: "darwin", Arch: "arm64"}, {OS: "linux", Arch: "amd64"}, {OS: "linux", Arch: "arm64"}, {OS: "windows", Arch: "amd64"}, {OS: "windows", Arch: "arm64"}}
}

func Run(ctx context.Context, config Config) ([]Artifact, error) {
	if err := validateConfig(&config); err != nil {
		return nil, err
	}
	stamp := time.Unix(config.Epoch, 0).UTC()
	if entries, err := os.ReadDir(config.Output); err == nil && len(entries) > 0 {
		return nil, fmt.Errorf("release output %s is not empty", config.Output)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect release output: %w", err)
	}
	if err := os.MkdirAll(config.Output, 0o755); err != nil {
		return nil, fmt.Errorf("create release output: %w", err)
	}
	temporary, err := os.MkdirTemp("", "taiga-releasepack-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(temporary) }()

	sbomName := fmt.Sprintf("taiga_%s_sbom.spdx.json", archiveVersion(config.Version))
	sbom, err := generateSBOM(ctx, config, stamp)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(config.Output, sbomName), sbom, 0o644); err != nil {
		return nil, fmt.Errorf("write SBOM: %w", err)
	}
	completions, err := generateCompletions(ctx, config, temporary)
	if err != nil {
		return nil, err
	}

	artifacts := make([]Artifact, 0, len(config.Targets)+1)
	for _, target := range config.Targets {
		artifact, err := buildTarget(ctx, config, target, stamp, temporary, sbom, completions)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	sbomArtifact, err := inspectArtifact(filepath.Join(config.Output, sbomName), "", "")
	if err != nil {
		return nil, err
	}
	artifacts = append(artifacts, sbomArtifact)
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Name < artifacts[j].Name })
	if err := writeChecksums(config.Output, artifacts); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func validateConfig(config *Config) error {
	if !versionPattern.MatchString(config.Version) {
		return fmt.Errorf("version %q must be semantic, for example v1.2.3", config.Version)
	}
	if !commitPattern.MatchString(config.Commit) {
		return fmt.Errorf("commit %q must be a hexadecimal Git object ID", config.Commit)
	}
	if config.Epoch <= 0 {
		return errors.New("source date epoch must be positive")
	}
	if err := config.Signing.validate(); err != nil {
		return err
	}
	root, err := filepath.Abs(config.RepoRoot)
	if err != nil {
		return err
	}
	config.RepoRoot = root
	if config.Output == "" {
		config.Output = filepath.Join(root, "dist", config.Version)
	} else if !filepath.IsAbs(config.Output) {
		config.Output = filepath.Join(root, config.Output)
	}
	if len(config.Targets) == 0 {
		config.Targets = DefaultTargets()
	}
	seen := map[string]bool{}
	for _, target := range config.Targets {
		key := target.OS + "/" + target.Arch
		if !targetPartPattern.MatchString(target.OS) || !targetPartPattern.MatchString(target.Arch) || seen[key] {
			return fmt.Errorf("invalid or duplicate target %q", key)
		}
		seen[key] = true
	}
	return nil
}

func buildTarget(ctx context.Context, config Config, target Target, stamp time.Time, temporary string, sbom []byte, completions map[string][]byte) (Artifact, error) {
	base := fmt.Sprintf("taiga_%s_%s_%s", archiveVersion(config.Version), target.OS, target.Arch)
	stage := filepath.Join(temporary, base)
	if err := os.MkdirAll(filepath.Join(stage, "completions"), 0o755); err != nil {
		return Artifact{}, err
	}
	binaryName := "taiga"
	if target.OS == "windows" {
		binaryName += ".exe"
	}
	binaryPath := filepath.Join(stage, binaryName)
	if err := buildBinary(ctx, config, target, binaryPath); err != nil {
		return Artifact{}, err
	}
	// Signing has to happen here: the archive, and therefore every checksum
	// derived from it, must cover the signed binary.
	if err := applySigning(ctx, config, target, binaryPath, temporary); err != nil {
		return Artifact{}, err
	}
	for _, name := range []string{"README.md", "README.zh-TW.md", "INSTALL.md", "INSTALL.zh-TW.md", "COMPATIBILITY.md", "COMPATIBILITY.zh-TW.md"} {
		data, err := os.ReadFile(filepath.Join(config.RepoRoot, name))
		if err != nil {
			return Artifact{}, fmt.Errorf("read %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(stage, name), data, 0o644); err != nil {
			return Artifact{}, err
		}
	}
	if data, err := os.ReadFile(filepath.Join(config.RepoRoot, "LICENSE")); err == nil {
		if err := os.WriteFile(filepath.Join(stage, "LICENSE"), data, 0o644); err != nil {
			return Artifact{}, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Artifact{}, fmt.Errorf("read LICENSE: %w", err)
	}
	if err := os.WriteFile(filepath.Join(stage, "SBOM.spdx.json"), sbom, 0o644); err != nil {
		return Artifact{}, err
	}
	for name, data := range completions {
		if err := os.WriteFile(filepath.Join(stage, "completions", name), data, 0o644); err != nil {
			return Artifact{}, err
		}
	}
	entries, err := collectEntries(stage, base)
	if err != nil {
		return Artifact{}, err
	}
	archivePath := filepath.Join(config.Output, base+".tar.gz")
	if target.OS == "windows" {
		archivePath = filepath.Join(config.Output, base+".zip")
		err = writeZip(archivePath, entries, stamp)
	} else {
		err = writeTarGzip(archivePath, entries, stamp)
	}
	if err != nil {
		return Artifact{}, err
	}
	return inspectArtifact(archivePath, target.OS, target.Arch)
}

func buildBinary(ctx context.Context, config Config, target Target, output string) error {
	buildDate := time.Unix(config.Epoch, 0).UTC().Format(time.RFC3339)
	ldflags := strings.Join([]string{"-s", "-w", "-buildid=", "-X", "github.com/KoukeNeko/taiga-cli/internal/version.Version=" + config.Version, "-X", "github.com/KoukeNeko/taiga-cli/internal/version.Commit=" + config.Commit, "-X", "github.com/KoukeNeko/taiga-cli/internal/version.BuildDate=" + buildDate}, " ")
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-ldflags", ldflags, "-o", output, "./cmd/taiga")
	command.Dir = config.RepoRoot
	command.Env = withEnvironment(os.Environ(), map[string]string{"CGO_ENABLED": "0", "GOOS": target.OS, "GOARCH": target.Arch, "GOAMD64": "v1", "GOARM64": "v8.0", "GOEXPERIMENT": "", "GOFLAGS": "-mod=readonly", "SOURCE_DATE_EPOCH": fmt.Sprint(config.Epoch)})
	if output, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("build %s/%s: %w: %s", target.OS, target.Arch, err, strings.TrimSpace(string(output)))
	}
	return os.Chmod(output, 0o755)
}

func generateCompletions(ctx context.Context, config Config, temporary string) (map[string][]byte, error) {
	host := Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
	binary := filepath.Join(temporary, "taiga-completion-host")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if err := buildBinary(ctx, config, host, binary); err != nil {
		return nil, err
	}
	files := map[string]string{"bash": "taiga.bash", "zsh": "_taiga", "fish": "taiga.fish", "powershell": "taiga.ps1"}
	result := map[string][]byte{}
	for shell, name := range files {
		command := exec.CommandContext(ctx, binary, "completion", shell)
		command.Dir = config.RepoRoot
		data, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("generate %s completion: %w", shell, err)
		}
		result[name] = data
	}
	return result, nil
}

type archiveEntry struct {
	Name string
	Mode os.FileMode
	Data []byte
}

func collectEntries(root, prefix string) ([]archiveEntry, error) {
	entries := []archiveEntry{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		entries = append(entries, archiveEntry{Name: filepath.ToSlash(filepath.Join(prefix, relative)), Mode: info.Mode().Perm(), Data: data})
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, err
}

func writeTarGzip(path string, entries []archiveEntry, stamp time.Time) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	gzipWriter, _ := gzip.NewWriterLevel(file, gzip.BestCompression)
	gzipWriter.ModTime = stamp
	gzipWriter.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.Name, Mode: int64(entry.Mode), Size: int64(len(entry.Data)), ModTime: stamp, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tarWriter.WriteHeader(header); err != nil {
			return closeWriters(file, tarWriter, gzipWriter, err)
		}
		if _, err := tarWriter.Write(entry.Data); err != nil {
			return closeWriters(file, tarWriter, gzipWriter, err)
		}
	}
	return closeWriters(file, tarWriter, gzipWriter, nil)
}

func closeWriters(file *os.File, tarWriter *tar.Writer, gzipWriter *gzip.Writer, first error) error {
	for _, err := range []error{tarWriter.Close(), gzipWriter.Close(), file.Close()} {
		if first == nil && err != nil {
			first = err
		}
	}
	return first
}

func writeZip(path string, entries []archiveEntry, stamp time.Time) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := zip.NewWriter(file)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.Name, Method: zip.Deflate}
		header.SetMode(entry.Mode)
		header.Modified = stamp
		part, err := writer.CreateHeader(header)
		if err == nil {
			_, err = part.Write(entry.Data)
		}
		if err != nil {
			_ = writer.Close()
			_ = file.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func inspectArtifact(path, goos, arch string) (Artifact, error) {
	file, err := os.Open(path)
	if err != nil {
		return Artifact{}, err
	}
	hash := sha256.New()
	bytesWritten, copyErr := io.Copy(hash, file)
	closeErr := file.Close()
	if copyErr != nil {
		return Artifact{}, copyErr
	}
	if closeErr != nil {
		return Artifact{}, closeErr
	}
	return Artifact{Name: filepath.Base(path), OS: goos, Arch: arch, SHA256: hex.EncodeToString(hash.Sum(nil)), Bytes: bytesWritten}, nil
}

func writeChecksums(output string, artifacts []Artifact) error {
	var builder strings.Builder
	for _, artifact := range artifacts {
		_, _ = fmt.Fprintf(&builder, "%s  %s\n", artifact.SHA256, artifact.Name)
	}
	return os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(builder.String()), 0o644)
}

type goModule struct {
	Path    string
	Version string
	Sum     string
	Main    bool
}

type spdxDocument struct {
	SPDXVersion       string             `json:"spdxVersion"`
	DataLicense       string             `json:"dataLicense"`
	SPDXID            string             `json:"SPDXID"`
	Name              string             `json:"name"`
	DocumentNamespace string             `json:"documentNamespace"`
	CreationInfo      spdxCreationInfo   `json:"creationInfo"`
	Packages          []spdxPackage      `json:"packages"`
	Relationships     []spdxRelationship `json:"relationships"`
}

type spdxCreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type spdxPackage struct {
	Name             string `json:"name"`
	SPDXID           string `json:"SPDXID"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
	LicenseConcluded string `json:"licenseConcluded"`
	LicenseDeclared  string `json:"licenseDeclared"`
	CopyrightText    string `json:"copyrightText"`
}

type spdxRelationship struct {
	Element string `json:"spdxElementId"`
	Type    string `json:"relationshipType"`
	Related string `json:"relatedSpdxElement"`
}

func generateSBOM(ctx context.Context, config Config, stamp time.Time) ([]byte, error) {
	command := exec.CommandContext(ctx, "go", "list", "-m", "-json", "all")
	command.Dir = config.RepoRoot
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list Go modules: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(output))
	modules := []goModule{}
	for {
		var module goModule
		if err := decoder.Decode(&module); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode Go module: %w", err)
		}
		modules = append(modules, module)
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })
	document := spdxDocument{SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT", Name: "taiga-cli-" + config.Version, DocumentNamespace: fmt.Sprintf("https://github.com/KoukeNeko/taiga-cli/releases/%s/sbom-%s", config.Version, config.Commit), CreationInfo: spdxCreationInfo{Created: stamp.Format(time.RFC3339), Creators: []string{"Tool: taiga-cli-releasepack"}}}
	rootID := ""
	for _, module := range modules {
		version := module.Version
		if module.Main {
			version = config.Version
		}
		id := "SPDXRef-Package-" + shortHash(module.Path+"@"+version)
		pkg := spdxPackage{Name: module.Path, SPDXID: id, VersionInfo: version, DownloadLocation: "NOASSERTION", FilesAnalyzed: false, LicenseConcluded: "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION"}
		document.Packages = append(document.Packages, pkg)
		if module.Main {
			rootID = id
			document.Relationships = append(document.Relationships, spdxRelationship{Element: "SPDXRef-DOCUMENT", Type: "DESCRIBES", Related: id})
		}
	}
	for _, pkg := range document.Packages {
		if pkg.SPDXID != rootID {
			document.Relationships = append(document.Relationships, spdxRelationship{Element: rootID, Type: "DEPENDS_ON", Related: pkg.SPDXID})
		}
	}
	data, err := json.MarshalIndent(document, "", "  ")
	return append(data, '\n'), err
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func archiveVersion(version string) string { return strings.TrimPrefix(version, "v") }

func withEnvironment(current []string, changes map[string]string) []string {
	result := make([]string, 0, len(current)+len(changes))
	for _, item := range current {
		key := strings.SplitN(item, "=", 2)[0]
		if _, changed := changes[key]; !changed {
			result = append(result, item)
		}
	}
	keys := make([]string, 0, len(changes))
	for key := range changes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+changes[key])
	}
	return result
}
