package cli

import (
	"fmt"
	"runtime"

	buildversion "github.com/KoukeNeko/taiga-cli/internal/version"
	"github.com/spf13/cobra"
)

func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show CLI build information",
		Args:  exactArgs(0),
		RunE: func(_ *cobra.Command, _ []string) error {
			info := buildversion.Info{
				Version:   buildversion.Version,
				Commit:    buildversion.Commit,
				BuildDate: buildversion.BuildDate,
				GoVersion: runtime.Version(),
				OS:        runtime.GOOS,
				Arch:      runtime.GOARCH,
			}
			if a.global.JSON {
				return a.renderer().Data(info)
			}
			_, _ = fmt.Fprintf(a.Out, "taiga %s\ncommit: %s\nbuilt: %s\ngo: %s\nplatform: %s/%s\n", info.Version, info.Commit, info.BuildDate, info.GoVersion, info.OS, info.Arch)
			return nil
		},
	}
}
