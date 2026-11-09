package cli

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Zenit-Hosting-de/devbox/internal/core"
	"github.com/Zenit-Hosting-de/devbox/internal/profile"
)

func (a *app) profileCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "manage profiles", Args: cobra.NoArgs}
	var src profile.Source
	var issuer, identity string
	add := &cobra.Command{
		Use: "add SOURCE", Short: "fetch a profile and add it to the user configuration", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src.Source = args[0]
			src.Trust = nil
			if issuer != "" || identity != "" {
				if issuer == "" || identity == "" {
					return &exitError{code: ExitInvalid, err: errors.New("--trust-issuer and --trust-identity go together")}
				}
				src.Trust = &profile.Trust{Issuer: issuer, Identity: identity}
			}
			return a.profileAdd(cmd, src)
		},
	}
	add.Flags().StringVar(&src.Ref, "ref", "", "Git branch, tag or commit (default: the HEAD of the remote)")
	add.Flags().StringVar(&src.Path, "path", "", "path of the profile file in a Git repository (default: "+profile.DefaultGitPath+")")
	add.Flags().StringVar(&issuer, "trust-issuer", "", "OIDC issuer of the required Sigstore signature")
	add.Flags().StringVar(&identity, "trust-identity", "", "identity that must have signed the profile (with --trust-issuer)")

	cmd.AddCommand(
		&cobra.Command{Use: "list", Short: "list the profiles", Args: cobra.NoArgs, RunE: a.profileList},
		add,
		&cobra.Command{Use: "show ID", Short: "show a profile", Args: cobra.ExactArgs(1), RunE: a.profileShow},
		&cobra.Command{Use: "update [ID]", Short: "fetch remote profiles again", Args: cobra.MaximumNArgs(1), RunE: a.profileUpdate},
		&cobra.Command{Use: "remove ID", Short: "remove a profile from the user configuration", Args: cobra.ExactArgs(1), RunE: a.profileRemove},
	)
	return cmd
}

func (a *app) profileList(_ *cobra.Command, _ []string) error {
	views, err := core.Profiles(a.coreEnv())
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(views)
	}
	tw := tabwriter.NewWriter(a.io.Stdout, 0, 4, 2, ' ', 0)
	for _, v := range views {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", orDash(v.ID, "-"), orDash(v.Version, "-"), v.Origin, v.Source.Source, profileState(v))
	}
	return tw.Flush()
}

func orDash(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// profileState is the short state of a profile for the list.
func profileState(v core.ProfileView) string {
	parts := []string{}
	if v.Problem != "" {
		return "problem: " + v.Problem
	}
	if v.Trust != nil {
		parts = append(parts, "signed")
	}
	if v.FetchedAt != "" {
		parts = append(parts, "fetched "+v.FetchedAt)
	}
	if len(parts) == 0 {
		return "ok"
	}
	return strings.Join(parts, ", ")
}

func (a *app) profileAdd(cmd *cobra.Command, src profile.Source) error {
	v, err := core.AddProfile(cmd.Context(), a.coreEnv(), src)
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(v)
	}
	_, err = fmt.Fprintf(a.io.Stdout, "%s: profile %s %s added (%d settings, %d locked)\n", v.ID, v.Name, v.Version, v.Settings, len(v.Locked))
	return err
}

func (a *app) profileShow(_ *cobra.Command, args []string) error {
	v, doc, err := core.Profile(a.coreEnv(), args[0])
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(struct {
			*core.ProfileView
			Profile map[string]any `json:"profile"`
		}{v, doc})
	}
	w := a.io.Stdout
	_, _ = fmt.Fprintf(w, "id:       %s\nname:     %s\nversion:  %s\norigin:   %s\nsource:   %s\n", orDash(v.ID, "-"), orDash(v.Name, "-"), orDash(v.Version, "-"), v.Origin, v.Source.Source)
	if v.Ref != "" {
		_, _ = fmt.Fprintf(w, "ref:      %s\n", v.Ref)
	}
	if v.Path != "" {
		_, _ = fmt.Fprintf(w, "path:     %s\n", v.Path)
	}
	if v.Trust != nil {
		_, _ = fmt.Fprintf(w, "signed by: %s (%s)\n", v.Trust.Identity, v.Trust.Issuer)
	}
	if v.FetchedAt != "" {
		_, _ = fmt.Fprintf(w, "fetched:  %s\n", v.FetchedAt)
	}
	if v.Problem != "" {
		_, _ = fmt.Fprintf(w, "problem:  %s\n", v.Problem)
		return nil
	}
	settings, _ := doc["settings"].(map[string]any)
	keys := make([]string, 0, len(settings))
	for k := range settings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		_, _ = fmt.Fprintln(w, "settings:")
	}
	for _, k := range keys {
		entry, _ := settings[k].(map[string]any)
		lock := ""
		if l, _ := entry["locked"].(bool); l {
			lock = " (locked)"
		}
		_, _ = fmt.Fprintf(w, "  %s = %s%s\n", k, compact(entry["value"]), lock)
	}
	for _, section := range []string{"credentials", "groups", "repositories"} {
		if m, ok := doc[section].(map[string]any); ok && len(m) > 0 {
			names := make([]string, 0, len(m))
			for n := range m {
				names = append(names, n)
			}
			sort.Strings(names)
			_, _ = fmt.Fprintf(w, "%s: %s\n", section, strings.Join(names, ", "))
		}
	}
	return nil
}

func (a *app) profileUpdate(cmd *cobra.Command, args []string) error {
	ref := ""
	if len(args) == 1 {
		ref = args[0]
	}
	updates, err := core.UpdateProfiles(cmd.Context(), a.coreEnv(), ref)
	if err != nil {
		return err
	}
	if a.jsonOut {
		if err := a.printJSON(updates); err != nil {
			return err
		}
	} else {
		for _, u := range updates {
			name := orDash(u.ID, u.Source.Source)
			switch {
			case u.Error != "":
				_, _ = fmt.Fprintf(a.io.Stdout, "%s: update failed, the previous copy stays: %s\n", name, u.Error)
			case u.Kind == profile.KindFile:
				_, _ = fmt.Fprintf(a.io.Stdout, "%s: local file, checked\n", name)
			case u.Changed:
				_, _ = fmt.Fprintf(a.io.Stdout, "%s: updated to %s (revision %s)\n", name, orDash(u.Version, "-"), shortRev(u.Revision))
			default:
				_, _ = fmt.Fprintf(a.io.Stdout, "%s: up to date (revision %s)\n", name, shortRev(u.Revision))
			}
		}
	}
	var failed []string
	for _, u := range updates {
		if u.Error != "" {
			failed = append(failed, orDash(u.ID, u.Source.Source))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("update failed for %s", strings.Join(failed, ", "))
	}
	return nil
}

func shortRev(rev string) string {
	if len(rev) > 12 {
		return rev[:12]
	}
	return orDash(rev, "-")
}

func (a *app) profileRemove(_ *cobra.Command, args []string) error {
	v, err := core.RemoveProfile(a.coreEnv(), args[0])
	if err != nil {
		return err
	}
	if a.jsonOut {
		return a.printJSON(v)
	}
	_, err = fmt.Fprintf(a.io.Stdout, "%s: profile removed\n", orDash(v.ID, v.Source.Source))
	return err
}
