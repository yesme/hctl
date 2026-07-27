package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/yesme/hctl/internal/buildcheck"
	"github.com/yesme/hctl/internal/derive"
	"github.com/yesme/hctl/internal/facts"
	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/identity"
)

const (
	ExitOK          = 0
	ExitGuard       = 1
	ExitUsage       = 2
	ExitWaitTimeout = 124
)

type App struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Now    func() time.Time
}

type globalOptions struct {
	RepoDir string
	Remote  string
	Seat    string
	Machine string
	Session string
	Command string
	Args    []string
}

type loaded struct {
	Repo     *gitx.Repo
	Snapshot *facts.Snapshot
	Result   *derive.Result
	Seat     string
	SeatErr  error
}

func (a *App) Run(ctx context.Context, argv []string) int {
	if a.Stdin == nil {
		a.Stdin = os.Stdin
	}
	if a.Stdout == nil {
		a.Stdout = os.Stdout
	}
	if a.Stderr == nil {
		a.Stderr = os.Stderr
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	options, err := parseGlobal(argv)
	if err != nil {
		return a.fail(ExitUsage, err)
	}
	switch options.Command {
	case "", "help", "--help", "-h":
		fmt.Fprint(a.Stdout, Help)
		return ExitOK
	case "version":
		info := buildcheck.Current()
		fmt.Fprintf(a.Stdout, "hctl %s revision=%s modified=%t go=%s\n",
			info.Version, valueOr(info.Revision, "unavailable"), info.Modified, info.Go)
		return ExitOK
	case "trailer":
		return a.commandTrailer(ctx, options, options.Args)
	case "seat":
		return a.commandSeat(ctx, options, options.Args)
	}
	repo, err := gitx.Open(ctx, options.RepoDir)
	if err != nil {
		return a.fail(ExitGuard, err)
	}
	load := a.load(ctx, repo, options)
	switch options.Command {
	case "status":
		return a.commandStatus(load, options.Args)
	case "doctor":
		return a.commandDoctor(ctx, load, options.Args)
	case "claim":
		return a.commandClaim(ctx, load, options, options.Args)
	case "begin":
		return a.commandBegin(ctx, load, options, options.Args)
	case "verdict":
		return a.commandVerdict(ctx, load, options, options.Args)
	case "merge":
		return a.commandMerge(ctx, load, options, options.Args)
	case "wait":
		return a.commandWait(ctx, repo, options, options.Args)
	default:
		return a.fail(ExitUsage, fmt.Errorf("unknown command %q; run hctl help", options.Command))
	}
}

func (a *App) load(ctx context.Context, repo *gitx.Repo, options globalOptions) *loaded {
	observer := facts.Observer{Repo: repo, Remote: options.Remote, Now: a.Now}
	snapshot := observer.Observe(ctx)
	var seat string
	var seatErr error
	if snapshot.Seats.Config.SchemaVersion == 1 {
		seat, seatErr = identity.DetectSeat(ctx, repo, snapshot.Seats.Config, options.Seat)
		if err := buildcheck.CheckKernelPin(snapshot.Seats.Config.Kernel, snapshot.Seats.Config.Enforcement); err != nil {
			snapshot.Problems = append(snapshot.Problems, facts.Problem{
				Code: "STALE_BINARY", Severity: facts.SeverityError, Detail: err.Error(),
			})
		} else if snapshot.Seats.Config.Kernel == "hctl@bootstrap" {
			snapshot.Problems = append(snapshot.Problems, facts.Problem{
				Code: "BOOTSTRAP_TRUST", Severity: facts.SeverityWarning,
				Detail: "kernel pin and enforcement are still bootstrap exceptions",
			})
		}
	}
	result := derive.Engine{Repo: repo, Now: a.Now}.Derive(ctx, snapshot, seat)
	return &loaded{Repo: repo, Snapshot: snapshot, Result: result, Seat: seat, SeatErr: seatErr}
}

func (a *App) fail(code int, err error) int {
	fmt.Fprintf(a.Stderr, "ERROR: %v\n", err)
	return code
}

func parseGlobal(argv []string) (globalOptions, error) {
	options := globalOptions{RepoDir: ".", Remote: os.Getenv("HCTL_REMOTE")}
	if options.Remote == "" {
		options.Remote = "origin"
	}
	i := 0
	for i < len(argv) {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			options.Command = arg
			options.Args = argv[i+1:]
			return options, nil
		}
		switch arg {
		case "-C":
			value, next, err := optionValue(argv, i, arg)
			if err != nil {
				return options, err
			}
			options.RepoDir, i = value, next
		case "--remote":
			value, next, err := optionValue(argv, i, arg)
			if err != nil {
				return options, err
			}
			options.Remote, i = value, next
		case "--seat":
			value, next, err := optionValue(argv, i, arg)
			if err != nil {
				return options, err
			}
			options.Seat, i = value, next
		case "--machine":
			value, next, err := optionValue(argv, i, arg)
			if err != nil {
				return options, err
			}
			options.Machine, i = value, next
		case "--session":
			value, next, err := optionValue(argv, i, arg)
			if err != nil {
				return options, err
			}
			options.Session, i = value, next
		default:
			return options, fmt.Errorf("unknown global option %q", arg)
		}
	}
	return options, nil
}

func optionValue(args []string, index int, name string) (string, int, error) {
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("%s requires a value", name)
	}
	return args[index+1], index + 2, nil
}

type parsedArgs struct {
	Positionals []string
	Values      map[string]string
	Bools       map[string]bool
}

func parseArgs(args []string, valueFlags, boolFlags map[string]bool) (parsedArgs, error) {
	result := parsedArgs{Values: map[string]string{}, Bools: map[string]bool{}}
	for i := 0; i < len(args); {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			result.Positionals = append(result.Positionals, arg)
			i++
			continue
		}
		if boolFlags[arg] {
			if result.Bools[arg] {
				return result, fmt.Errorf("option %s was repeated", arg)
			}
			result.Bools[arg] = true
			i++
			continue
		}
		if valueFlags[arg] {
			if _, duplicate := result.Values[arg]; duplicate {
				return result, fmt.Errorf("option %s was repeated", arg)
			}
			value, next, err := optionValue(args, i, arg)
			if err != nil {
				return result, err
			}
			result.Values[arg] = value
			i = next
			continue
		}
		return result, fmt.Errorf("unknown option %q", arg)
	}
	return result, nil
}

func parsePositiveInt(value, name string) (int, error) {
	number, err := strconv.Atoi(value)
	if err != nil || number < 1 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return number, nil
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}

func requireOne(values []string, usage string) (string, error) {
	if len(values) != 1 {
		return "", errors.New(usage)
	}
	return values[0], nil
}
