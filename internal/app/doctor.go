package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yesme/hctl/internal/buildcheck"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/identity"
)

type doctorCheck struct {
	Name   string
	Status string
	Detail string
}

func (a *App) commandDoctor(ctx context.Context, load *loaded, args []string) int {
	if len(args) != 0 {
		return a.fail(ExitUsage, fmt.Errorf("usage: hctl doctor"))
	}
	var checks []doctorCheck
	add := func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}
	add("repository", "ok", load.Repo.Root)
	if load.Result.Complete {
		add("facts", "ok", fmt.Sprintf("main=%s assignments=%d chains=%d",
			load.Snapshot.MainCommit, len(load.Snapshot.Assignments), len(load.Snapshot.Chains)))
	} else {
		add("facts", "error", "remote observation or replay is incomplete; see problems below")
	}
	if load.SeatErr != nil {
		add("seat", "error", load.SeatErr.Error())
	} else {
		add("seat", "ok", load.Seat)
	}
	if load.Snapshot.Seats.Config.SchemaVersion == 1 {
		add("config", "ok", fmt.Sprintf(
			"schema=1 coordinator=%s enforcement=%s source=%s:%s",
			load.Snapshot.Seats.Config.MergeCoordinator,
			load.Snapshot.Seats.Config.Enforcement,
			load.Snapshot.Seats.Source.Commit,
			load.Snapshot.Seats.Source.Blob,
		))
		if err := buildcheck.CheckKernelPin(
			load.Snapshot.Seats.Config.Kernel,
			load.Snapshot.Seats.Config.Enforcement,
		); err != nil {
			add("kernel-pin", "error", err.Error())
		} else if load.Snapshot.Seats.Config.Kernel == "hctl@bootstrap" {
			add("kernel-pin", "warning", "hctl@bootstrap is allowed only before activation")
		} else {
			info := buildcheck.Current()
			add("kernel-pin", "ok", fmt.Sprintf("pin=%s binary=%s", load.Snapshot.Seats.Config.Kernel, info.Revision))
		}
		if load.Snapshot.Seats.Config.Enforcement == "bootstrap" {
			add("enforcement", "warning", "bootstrap trust exception remains active")
		} else {
			add("enforcement", "ok", "active")
		}
	}
	if err := buildcheck.CheckGoDirective(filepath.Join(load.Repo.Root, "go.mod")); err != nil {
		add("go-version", "error", err.Error())
	} else {
		add("go-version", "ok", buildcheck.Current().Go)
	}
	refspecStatus, refspecDetail := checkCoopRefspec(ctx, load)
	add("coop-refspec", refspecStatus, refspecDetail)
	hookStatus, hookDetail := checkHooks(ctx, load.Repo)
	add("hooks", hookStatus, hookDetail)
	ghStatus, ghDetail := checkGHAuth(ctx, load.Repo.Root)
	add("gh-auth", ghStatus, ghDetail)
	attrStatus, attrDetail := checkAttribution(load)
	add("attribution", attrStatus, attrDetail)

	hasError := false
	for _, check := range checks {
		fmt.Fprintf(a.Stdout, "%-14s %-7s %s\n", check.Name, check.Status, check.Detail)
		if check.Status == "error" {
			hasError = true
		}
	}
	for _, problem := range load.Result.Problems {
		fmt.Fprintf(a.Stdout, "problem        %-7s %s: %s\n", problem.Severity, problem.Code, problem.Detail)
	}
	if hasError {
		return ExitGuard
	}
	return ExitOK
}

func checkCoopRefspec(ctx context.Context, load *loaded) (string, string) {
	out, err := load.Repo.Output(ctx, "config", "--get-all", "remote."+load.Snapshot.Remote+".fetch")
	if err != nil && gitx.ExitCode(err) != 1 {
		return "error", err.Error()
	}
	want := "refs/coop/*:refs/hctl/remotes/" + load.Snapshot.Remote + "/coop/*"
	for _, line := range strings.Split(out, "\n") {
		if line == want {
			return "ok", want + " (non-force)"
		}
		if strings.TrimPrefix(line, "+") == want && strings.HasPrefix(line, "+") {
			return "error", "coop tracking refspec must not use '+'; rollback must fail loudly"
		}
	}
	status := "warning"
	if load.Snapshot.Seats.Config.Enforcement == "active" {
		status = "error"
	}
	return status, fmt.Sprintf("missing %q; add it with git config --add remote.%s.fetch", want, load.Snapshot.Remote)
}

func checkHooks(ctx context.Context, repo *gitx.Repo) (string, string) {
	path := ""
	if out, err := repo.Output(ctx, "config", "--get", "core.hooksPath"); err == nil {
		path = strings.TrimSpace(out)
		if !filepath.IsAbs(path) {
			path = filepath.Join(repo.Root, path)
		}
	} else {
		path = filepath.Join(repo.CommonDir, "hooks")
	}
	prePush := filepath.Join(path, "pre-push")
	info, err := os.Stat(prePush)
	if err == nil && info.Mode()&0o111 != 0 {
		return "ok", prePush
	}
	return "warning", "no executable pre-push hook found (level-3 diagnostic, not a correctness proof)"
}

func checkGHAuth(ctx context.Context, dir string) (string, string) {
	path, err := exec.LookPath("gh")
	if err != nil {
		return "error", "gh is not installed"
	}
	cmd := exec.CommandContext(ctx, path, "auth", "status")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "error", strings.TrimSpace(string(out))
	}
	return "ok", firstLine(string(out))
}

func checkAttribution(load *loaded) (string, string) {
	if load.Snapshot.Seats.Config.SchemaVersion != 1 {
		return "warning", "seats configuration unavailable; attribution chain not checked"
	}
	if load.SeatErr != nil {
		return "warning", "seat unresolved; attribution checks skipped (" + load.SeatErr.Error() + ")"
	}
	if load.Seat == "" {
		return "warning", "seat unknown; pass --seat or run from a seat worktree, then hctl seat init"
	}
	local, err := identity.LoadLocal(load.Repo)
	if err != nil {
		return "error", err.Error()
	}
	// Same shared ResolveCoauthorEmail as trailer/seat init (ownership + seat match).
	email, source, err := ResolveCoauthorEmail(load.Seat, load.Snapshot.Seats.Config, local)
	if err != nil {
		status := "warning"
		if load.Snapshot.Seats.Config.Enforcement == "active" {
			status = "error"
		}
		return status, err.Error() + "; run hctl seat init or set seats.toml coauthor_email"
	}
	detail := fmt.Sprintf("seat=%s email=%s source=%s", load.Seat, email, source)
	if source == "seats" && local.CoauthorEmail == "" {
		detail += "; recommend hctl seat init for local persistence"
	}
	return "ok", detail
}

func firstLine(value string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(value), "\n")
	return line
}
