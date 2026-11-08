package engine

import (
	"context"
	"strings"
)

type DoctorResult struct {
	OK         bool                    `json:"ok"`
	Issues     []string                `json:"issues,omitempty"`
	Entrypoint *EntrypointDoctorResult `json:"entrypoint,omitempty"`
	GitHooks   *HooksResult            `json:"git_hooks,omitempty"`
}

func (r DoctorResult) Text() string {
	if r.OK {
		return "Doctor status: ok\n"
	}
	lines := []string{"Doctor status: failed"}
	for _, issue := range r.Issues {
		lines = append(lines, "- "+issue)
	}
	return strings.Join(lines, "\n") + "\n"
}

func (a *App) Doctor(ctx context.Context) (*DoctorResult, error) {
	entrypoint, err := a.EntrypointDoctor()
	if err != nil {
		return nil, err
	}
	result := &DoctorResult{Entrypoint: entrypoint, OK: entrypoint.OK}
	result.Issues = append(result.Issues, entrypoint.Issues...)

	// Git hooks are only applicable to projects inside a Git work tree.
	if _, err := runGit(ctx, "-C", a.Root, "rev-parse", "--is-inside-work-tree"); err == nil {
		hooks, err := a.HooksStatus(ctx)
		if err != nil {
			return nil, err
		}
		result.GitHooks = hooks
		if !hooks.Enabled {
			result.OK = false
			result.Issues = append(result.Issues, "Git hooks are not enabled; run `ctxpm install`")
		}
	}
	return result, nil
}
