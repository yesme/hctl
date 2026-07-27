package app

import (
	"fmt"
	"os"
	"strings"
)

func (a *App) commandTrailer(args []string) int {
	if len(args) != 0 {
		return a.fail(ExitUsage, fmt.Errorf("usage: hctl trailer"))
	}
	model := strings.TrimSpace(os.Getenv("HCTL_MODEL_DISPLAY"))
	effort := strings.TrimSpace(os.Getenv("HCTL_REASONING_EFFORT"))
	email := strings.TrimSpace(os.Getenv("HCTL_COAUTHOR_EMAIL"))
	if model == "" || effort == "" || email == "" {
		return a.fail(ExitGuard, fmt.Errorf(
			"runtime attribution unavailable; set HCTL_MODEL_DISPLAY, HCTL_REASONING_EFFORT, and HCTL_COAUTHOR_EMAIL (D-23 fails closed)",
		))
	}
	if strings.ContainsAny(model+effort+email, "\r\n<>") || !strings.Contains(email, "@") {
		return a.fail(ExitGuard, fmt.Errorf("runtime attribution contains unsafe trailer characters"))
	}
	fmt.Fprintf(a.Stdout, "Co-authored-by: %s (%s effort) <%s>\n", model, effort, email)
	return ExitOK
}
