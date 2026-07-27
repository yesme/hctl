package app

import (
	"fmt"
	"strings"

	"github.com/yesme/hctl/internal/derive"
	"github.com/yesme/hctl/internal/facts"
)

type statusDocument struct {
	SchemaVersion int                       `json:"schema_version"`
	Remote        string                    `json:"remote"`
	Main          string                    `json:"main,omitempty"`
	Seat          string                    `json:"seat,omitempty"`
	Complete      bool                      `json:"complete"`
	FactTips      map[string]string         `json:"fact_tips"`
	Problems      []facts.Problem           `json:"problems"`
	Obligations   []*derive.ObligationState `json:"obligations"`
}

func (a *App) commandStatus(load *loaded, args []string) int {
	parsed, err := parseArgs(args, nil, map[string]bool{"--json": true})
	if err != nil || len(parsed.Positionals) != 0 {
		if err == nil {
			err = fmt.Errorf("usage: hctl status [--json]")
		}
		return a.fail(ExitUsage, err)
	}
	if parsed.Bools["--json"] {
		document := statusDocument{
			SchemaVersion: 1,
			Remote:        load.Snapshot.Remote,
			Main:          load.Snapshot.MainCommit,
			Seat:          load.Seat,
			Complete:      load.Result.Complete,
			FactTips:      load.Result.FactTips,
			Problems:      load.Result.Problems,
			Obligations:   load.Result.Obligations,
		}
		if err := writeJSON(a.Stdout, document); err != nil {
			return a.fail(ExitGuard, err)
		}
	} else {
		a.printStatus(load)
	}
	if !load.Result.Complete {
		return ExitGuard
	}
	return ExitOK
}

func (a *App) printStatus(load *loaded) {
	fmt.Fprintf(a.Stdout, "hctl status  remote=%s  main=%s  seat=%s  facts=%s\n",
		load.Snapshot.Remote, valueOr(load.Snapshot.MainCommit, "<unavailable>"),
		valueOr(load.Seat, "<unknown>"), completeWord(load.Result.Complete))
	for _, problem := range load.Result.Problems {
		fmt.Fprintf(a.Stdout, "problem  severity=%s  code=%s  seat=%s\n  %s\n",
			problem.Severity, problem.Code, valueOr(problem.Seat, "-"), problem.Detail)
	}
	if len(load.Result.Obligations) == 0 {
		fmt.Fprintln(a.Stdout, "obligations: none (or facts are incomplete)")
		return
	}
	for _, state := range load.Result.Obligations {
		claim := "-"
		if state.Claim != nil {
			claim = state.Claim.OID
		}
		gate := ""
		if state.Kind == "gate" {
			gate = fmt.Sprintf(" gate=%s mode=%s threshold=%s", state.GateID, state.GateMode, state.Threshold)
		}
		fmt.Fprintf(a.Stdout,
			"obligation  id=%s\n  assignment=%s kind=%s holder=%s%s state=%s claim=%s\n  next=%s\n",
			state.Obligation.ID, state.Assignment, state.Kind, state.Holder, gate, state.State, claim,
			strings.TrimSpace(state.NextAction),
		)
		if state.Carried != nil {
			fmt.Fprintf(a.Stdout,
				"  carried: %s\n  original verdict=%s revision={base:%s,head:%s} report={commit:%s,path:%s,blob:%s}\n",
				state.Carried.Kind, state.Carried.Verdict,
				state.Carried.Revision.Base, state.Carried.Revision.Head,
				state.Carried.Report.Commit, state.Carried.Report.Path, state.Carried.Report.Blob,
			)
		}
	}
}

func completeWord(value bool) string {
	if value {
		return "complete"
	}
	return "INCOMPLETE"
}
