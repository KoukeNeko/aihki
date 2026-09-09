package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/KoukeNeko/taiga-cli/internal/releasepack"
)

func main() {
	version := flag.String("version", "", "semantic release version, for example v1.2.3")
	commit := flag.String("commit", "", "Git commit included in the release")
	epoch := flag.Int64("source-date-epoch", 0, "reproducible build timestamp")
	output := flag.String("output", "", "release output directory")
	targets := flag.String("targets", "", "comma-separated GOOS/GOARCH targets")
	signIdentity := flag.String("sign-identity", "", "macOS Developer ID Application identity; empty disables signing")
	signKeychain := flag.String("sign-keychain", "", "keychain searched for the signing identity")
	notaryKey := flag.String("notary-key", "", "App Store Connect API key file used for notarization")
	notaryKeyID := flag.String("notary-key-id", "", "App Store Connect API key ID")
	notaryIssuer := flag.String("notary-issuer", "", "App Store Connect API issuer ID")
	flag.Parse()
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	config := releasepack.Config{
		RepoRoot: cwd,
		Output:   *output,
		Version:  *version,
		Commit:   *commit,
		Epoch:    *epoch,
		Signing: releasepack.Signing{
			Identity:     *signIdentity,
			Keychain:     *signKeychain,
			NotaryKey:    *notaryKey,
			NotaryKeyID:  *notaryKeyID,
			NotaryIssuer: *notaryIssuer,
		},
	}
	if *targets != "" {
		for _, raw := range strings.Split(*targets, ",") {
			parts := strings.Split(strings.TrimSpace(raw), "/")
			if len(parts) != 2 {
				fatal(fmt.Errorf("invalid target %q", raw))
			}
			config.Targets = append(config.Targets, releasepack.Target{OS: parts[0], Arch: parts[1]})
		}
	}
	artifacts, err := releasepack.Run(context.Background(), config)
	if err != nil {
		fatal(err)
	}
	for _, artifact := range artifacts {
		fmt.Printf("%s  %s\n", artifact.SHA256, artifact.Name)
	}
}

func fatal(err error) {
	_, _ = fmt.Fprintln(os.Stderr, "releasepack:", err)
	os.Exit(1)
}
