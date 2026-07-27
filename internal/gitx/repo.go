package gitx

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// Repo is a thin, argv-safe wrapper around git. It intentionally exposes
// plumbing operations instead of reproducing Git's object and ref semantics.
type Repo struct {
	Root      string
	CommonDir string
	GitBin    string
}

func Open(ctx context.Context, dir string) (*Repo, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("absolute repository path: %w", err)
	}
	r := &Repo{Root: abs, GitBin: "git"}
	root, err := r.Output(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("not a git worktree: %w", err)
	}
	r.Root = strings.TrimSpace(root)
	common, err := r.Output(ctx, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return nil, fmt.Errorf("resolve git common directory: %w", err)
	}
	r.CommonDir = strings.TrimSpace(common)
	return r, nil
}

type CommandError struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *CommandError) Error() string {
	detail := strings.TrimSpace(e.Stderr)
	if detail == "" {
		detail = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), detail)
}

func (e *CommandError) Unwrap() error { return e.Err }

func (r *Repo) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, r.GitBin, args...)
	cmd.Dir = r.Root
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return cmd
}

func (r *Repo) Output(ctx context.Context, args ...string) (string, error) {
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &CommandError{Args: append([]string(nil), args...), Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

func (r *Repo) Run(ctx context.Context, stdin []byte, args ...string) (string, error) {
	cmd := r.command(ctx, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", &CommandError{Args: append([]string(nil), args...), Stderr: stderr.String(), Err: err}
	}
	return stdout.String(), nil
}

func ExitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	var ce *CommandError
	if errors.As(err, &ce) && errors.As(ce.Err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

func (r *Repo) Resolve(ctx context.Context, ref string) (string, bool, error) {
	out, err := r.Output(ctx, "rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		if ExitCode(err) == 128 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

func (r *Repo) ResolveObject(ctx context.Context, spec string) (string, bool, error) {
	out, err := r.Output(ctx, "rev-parse", "--verify", "--end-of-options", spec)
	if err != nil {
		if ExitCode(err) == 128 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

func (r *Repo) ObjectFormat(ctx context.Context) (string, error) {
	out, err := r.Output(ctx, "rev-parse", "--show-object-format")
	if err != nil {
		return "", err
	}
	format := strings.TrimSpace(out)
	if format != "sha1" && format != "sha256" {
		return "", fmt.Errorf("unsupported Git object format %q", format)
	}
	return format, nil
}

func (r *Repo) HashObject(ctx context.Context, data []byte) (string, error) {
	out, err := r.Run(ctx, data, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) MakeSingleFileTree(ctx context.Context, path, blob string) (string, error) {
	if path == "" || strings.ContainsAny(path, "\x00\n\t") {
		return "", fmt.Errorf("unsafe tree path %q", path)
	}
	line := []byte("100644 blob " + blob + "\t" + path + "\n")
	out, err := r.Run(ctx, line, "mktree")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) CommitTree(ctx context.Context, tree, parent, message string) (string, error) {
	args := []string{"commit-tree", tree}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	args = append(args, "-m", message)
	out, err := r.Output(ctx, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (r *Repo) UpdateRef(ctx context.Context, ref, newOID, oldOID, message string) error {
	args := []string{"update-ref"}
	if message != "" {
		args = append(args, "-m", message)
	}
	args = append(args, ref, newOID)
	// An explicit empty old OID means "the ref must not exist".
	args = append(args, oldOID)
	_, err := r.Output(ctx, args...)
	return err
}

func (r *Repo) DeleteRef(ctx context.Context, ref, oldOID, message string) error {
	args := []string{"update-ref", "-d"}
	if message != "" {
		args = append(args, "-m", message)
	}
	args = append(args, ref)
	if oldOID != "" {
		args = append(args, oldOID)
	}
	_, err := r.Output(ctx, args...)
	return err
}

func (r *Repo) IsAncestor(ctx context.Context, older, newer string) (bool, error) {
	if older == "" || newer == "" {
		return false, nil
	}
	_, err := r.Output(ctx, "merge-base", "--is-ancestor", older, newer)
	if err == nil {
		return true, nil
	}
	if ExitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

func (r *Repo) BlobAt(ctx context.Context, commit, path string) (string, []byte, error) {
	spec := commit + ":" + path
	oid, ok, err := r.ResolveObject(ctx, spec)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, fmt.Errorf("%s does not contain %s", commit, path)
	}
	out, err := r.Output(ctx, "cat-file", "blob", oid)
	if err != nil {
		return "", nil, err
	}
	return oid, []byte(out), nil
}

func (r *Repo) CurrentBranch(ctx context.Context) (string, bool, error) {
	out, err := r.Output(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if ExitCode(err) == 1 {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

func (r *Repo) Switch(ctx context.Context, branch string) error {
	_, err := r.Output(ctx, "switch", branch)
	return err
}

func (r *Repo) CreateAndSwitch(ctx context.Context, branch, start string) error {
	_, err := r.Output(ctx, "switch", "-c", branch, start)
	return err
}

func (r *Repo) CreateBranch(ctx context.Context, branch, start string) error {
	_, err := r.Output(ctx, "branch", branch, start)
	return err
}

func (r *Repo) WorktreeClean(ctx context.Context) (bool, error) {
	out, err := r.Output(ctx, "status", "--porcelain=v1", "--untracked-files=normal")
	if err != nil {
		return false, err
	}
	return out == "", nil
}

// ListTree returns all blob entries below prefix in one ls-tree invocation.
func (r *Repo) ListTree(ctx context.Context, commit, prefix string) ([]TreeEntry, error) {
	args := []string{"ls-tree", "-r", "-z", commit}
	if prefix != "" {
		args = append(args, "--", prefix)
	}
	out, err := r.Output(ctx, args...)
	if err != nil {
		return nil, err
	}
	var entries []TreeEntry
	for _, record := range strings.Split(out, "\x00") {
		if record == "" {
			continue
		}
		meta, path, ok := strings.Cut(record, "\t")
		if !ok {
			return nil, fmt.Errorf("malformed ls-tree record")
		}
		fields := strings.Fields(meta)
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed ls-tree metadata %q", meta)
		}
		entries = append(entries, TreeEntry{Mode: fields[0], Type: fields[1], OID: fields[2], Path: path})
	}
	return entries, nil
}

type TreeEntry struct {
	Mode string
	Type string
	OID  string
	Path string
}

// Batch provides one long-lived git cat-file --batch process. Callers can
// inspect every event commit/tree/blob without spawning per-object processes.
type Batch struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	stderr bytes.Buffer
	mu     sync.Mutex
	closed bool
}

type Object struct {
	OID  string
	Type string
	Data []byte
}

func (r *Repo) NewBatch(ctx context.Context) (*Batch, error) {
	cmd := r.command(ctx, "cat-file", "--batch")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	b := &Batch{cmd: cmd, stdin: stdin, reader: bufio.NewReader(stdout)}
	cmd.Stderr = &b.stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	return b, nil
}

func (b *Batch) Read(spec string) (Object, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return Object{}, errors.New("cat-file batch is closed")
	}
	if strings.ContainsAny(spec, "\x00\n\r") {
		return Object{}, fmt.Errorf("unsafe object spec %q", spec)
	}
	if _, err := io.WriteString(b.stdin, spec+"\n"); err != nil {
		return Object{}, err
	}
	header, err := b.reader.ReadString('\n')
	if err != nil {
		return Object{}, fmt.Errorf("read cat-file header: %w", err)
	}
	header = strings.TrimSuffix(header, "\n")
	if strings.HasSuffix(header, " missing") {
		return Object{}, fmt.Errorf("object %q is missing", spec)
	}
	fields := strings.Fields(header)
	if len(fields) != 3 {
		return Object{}, fmt.Errorf("malformed cat-file header %q", header)
	}
	size, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || size < 0 {
		return Object{}, fmt.Errorf("malformed cat-file object size %q", fields[2])
	}
	data := make([]byte, size)
	if _, err := io.ReadFull(b.reader, data); err != nil {
		return Object{}, fmt.Errorf("read cat-file payload: %w", err)
	}
	trailer, err := b.reader.ReadByte()
	if err != nil {
		return Object{}, fmt.Errorf("read cat-file trailer: %w", err)
	}
	if trailer != '\n' {
		return Object{}, fmt.Errorf("malformed cat-file trailer")
	}
	return Object{OID: fields[0], Type: fields[1], Data: data}, nil
}

func (b *Batch) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	_ = b.stdin.Close()
	b.mu.Unlock()
	err := b.cmd.Wait()
	if err != nil {
		return fmt.Errorf("git cat-file --batch: %s: %w", strings.TrimSpace(b.stderr.String()), err)
	}
	return nil
}

// ParseCommit extracts the structural headers needed by the event-chain
// reader. GPG signatures and unknown headers are retained by Git but irrelevant.
type Commit struct {
	Tree          string
	Parents       []string
	Author        string
	CommitterUnix int64
	Message       string
}

func ParseCommit(raw []byte) (Commit, error) {
	headers, message, ok := bytes.Cut(raw, []byte("\n\n"))
	if !ok {
		return Commit{}, errors.New("commit object has no header terminator")
	}
	var c Commit
	var lastKey string
	for _, line := range bytes.Split(headers, []byte("\n")) {
		if len(line) > 0 && line[0] == ' ' {
			// Continuation line (for example gpgsig).
			if lastKey == "" {
				return Commit{}, errors.New("orphan commit header continuation")
			}
			continue
		}
		key, value, found := bytes.Cut(line, []byte(" "))
		if !found {
			return Commit{}, fmt.Errorf("malformed commit header %q", line)
		}
		lastKey = string(key)
		switch lastKey {
		case "tree":
			c.Tree = string(value)
		case "parent":
			c.Parents = append(c.Parents, string(value))
		case "author":
			c.Author = string(value)
		case "committer":
			parts := bytes.Fields(value)
			if len(parts) >= 2 {
				ts, err := strconv.ParseInt(string(parts[len(parts)-2]), 10, 64)
				if err == nil {
					c.CommitterUnix = ts
				}
			}
		}
	}
	if c.Tree == "" {
		return Commit{}, errors.New("commit object has no tree")
	}
	c.Message = string(message)
	return c, nil
}

// ParseSingleFileTree validates the exact event tree shape and returns the blob
// OID. Git tree objects encode object IDs as raw bytes.
func ParseSingleFileTree(raw []byte, oidBytes int, wantName string) (string, error) {
	if oidBytes != 20 && oidBytes != 32 {
		return "", fmt.Errorf("unsupported raw object id length %d", oidBytes)
	}
	i := bytes.IndexByte(raw, 0)
	if i < 0 {
		return "", errors.New("tree entry has no NUL separator")
	}
	header := string(raw[:i])
	mode, name, ok := strings.Cut(header, " ")
	if !ok || mode != "100644" || name != wantName {
		return "", fmt.Errorf("tree must contain only 100644 %s", wantName)
	}
	start := i + 1
	end := start + oidBytes
	if end != len(raw) {
		return "", fmt.Errorf("tree must contain exactly one entry")
	}
	return fmt.Sprintf("%x", raw[start:end]), nil
}

func AtomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".hctl-write-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		_ = f.Close()
		if !ok {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(mode); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}
