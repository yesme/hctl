package app

const Help = `hctl — harness collaboration kernel

USAGE
  hctl [global options] <command> [arguments]

P1 COMMANDS
  hctl doctor
      Check repository configuration, seat identity, ref observation, hooks,
      GitHub auth, Go version, enforcement, and the binary kernel pin.

  hctl status [--json]
      Fetch the ref plane and reconcile all assignments, claims, verdicts,
      quorum expressions, and the merge slot. Every row includes the full
      obligation ID, holder, state, and next action.

  hctl claim <obligation> [--reclaim <claim-oid>]
      Acquire an obligation by local update-ref CAS followed by a remote
      force-with-lease CAS. The remote success is the fencing-token
      linearization point. Reclaim is always explicit.

  hctl begin <assignment> [--adopt <branch> | --draft-only]
      Formal by default: acquire/reuse the author claim before creating or
      switching to work/<seat>/<slug>. --draft-only creates local work without
      a claim and prints a warning.

  hctl verdict <gate-obligation> --decision <APPROVE|REQUEST_CHANGES>
      --report <path|commit:path> --completeness <COMPLETE|INCOMPLETE>
      [--scope <full|JSON|@file>] [--incomplete-reason <text>] [--pr <number>]
      Publish an exact-revision verdict fenced by the active gate claim. The
      report commit must already be reachable from the revision base.

  hctl merge <pr> [--check]
      The configured coordinator rechecks exact revision, required quorum,
      capacity=1, config pin, and receipt. --check prints the receipt without
      calling GitHub. The mutating form performs a matched-head squash merge
      and post-verifies parent, tree, and receipt.

  hctl wait [--timeout <duration>] [--interval <duration>] [--json]
      Foreground-only basic wait. Returns immediately for current attention;
      otherwise polls the remote ref plane silently until attention changes or
      timeout. Minimum interval is 250ms.

  hctl trailer
      Print a Co-authored-by trailer from HCTL_MODEL_DISPLAY and
      HCTL_REASONING_EFFORT plus HCTL_COAUTHOR_EMAIL. Missing runtime
      attribution fails closed.

  hctl version
  hctl help

GLOBAL OPTIONS
  -C <path>          repository worktree (default ".")
  --remote <name>    Git remote (default HCTL_REMOTE or "origin")
  --seat <seat>      explicit seat (default branch/worktree detection)
  --machine <id>     provenance machine id (default HCTL_MACHINE or persisted UUID)
  --session <id>     provenance session id (default HCTL_SESSION or null)

EXIT CODES
  0    success / attention available
  1    guard refusal, incomplete facts, or operational failure
  2    usage error
  124  wait timeout with no attention change

FACTS AND SAFETY
  Coordination facts live only on refs/coop/<seat>. memory/ and PR comments are
  opaque to the kernel. Unknown semantics, corrupt chains, incomplete remote
  observation, stale claims, and receipt mismatches all fail closed.
`
