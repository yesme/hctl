package facts

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/yesme/hctl/internal/gitx"
	"github.com/yesme/hctl/internal/protocol"
)

// PopulateIndexes builds the commit graph, main reachability, source-blob, and
// commit-tree caches in one rev-list plus one cat-file --batch process. Roots
// include every observed ref and every revision anchor retained by a known
// event, so CLAIM progress classification never degenerates into per-event Git
// subprocesses. Receipt replay uses the same path for a historical fact prefix.
func PopulateIndexes(ctx context.Context, repo *gitx.Repo, snapshot *Snapshot) error {
	if snapshot.MainCommit == "" {
		return fmt.Errorf("main commit is unavailable")
	}
	sources := map[string]protocol.Source{}
	potentialRoots := map[string]bool{snapshot.MainCommit: true}
	requiredRoots := map[string]bool{snapshot.MainCommit: true}
	addSource := func(source *protocol.Source) {
		if source != nil {
			sources[SourceKey(*source)] = *source
			potentialRoots[source.Commit] = true
		}
	}
	addRevision := func(revision *protocol.Revision) {
		if revision != nil {
			potentialRoots[revision.Base] = true
			potentialRoots[revision.Head] = true
		}
	}
	for _, oid := range snapshot.BranchTips {
		potentialRoots[oid] = true
		requiredRoots[oid] = true
	}
	for _, oid := range snapshot.PullTips {
		potentialRoots[oid] = true
		requiredRoots[oid] = true
	}
	for _, chain := range snapshot.Chains {
		for _, event := range chain.Events {
			switch {
			case event.Claim != nil:
				addSource(event.Claim.Obligation.Source)
				addSource(event.Claim.CoordinatorConfig)
				addRevision(event.Claim.RevisionAtClaim)
				if event.Claim.TipAtClaim != nil {
					potentialRoots[*event.Claim.TipAtClaim] = true
				}
			case event.Verdict != nil:
				addSource(event.Verdict.Obligation.Source)
				addSource(&event.Verdict.Report)
				addRevision(&event.Verdict.Revision)
			case event.Cancel != nil && event.Cancel.Target.Assignment != nil:
				addSource(event.Cancel.Target.Assignment)
			}
		}
	}
	addSource(&snapshot.Seats.Source)
	for _, record := range snapshot.Assignments {
		addSource(&record.Source)
	}
	batch, err := repo.NewBatch(ctx)
	if err != nil {
		return err
	}
	defer batch.Close()

	snapshot.CommitTrees = map[string]string{}
	snapshot.MissingCommits = map[string]bool{}
	validRoots := make([]string, 0, len(potentialRoots))
	rootOIDs := make([]string, 0, len(potentialRoots))
	for oid := range potentialRoots {
		rootOIDs = append(rootOIDs, oid)
	}
	sort.Strings(rootOIDs)
	for _, oid := range rootOIDs {
		object, err := batch.Read(oid)
		if err != nil || object.Type != "commit" {
			if requiredRoots[oid] {
				if err != nil {
					return fmt.Errorf("observed ref target %s is not readable as a commit: %w", oid, err)
				}
				return fmt.Errorf("observed ref target %s is %s, not a commit", oid, object.Type)
			}
			if err != nil && strings.Contains(err.Error(), "missing") {
				snapshot.MissingCommits[oid] = true
			}
			continue
		}
		commit, err := gitx.ParseCommit(object.Data)
		if err != nil {
			if requiredRoots[oid] {
				return fmt.Errorf("parse observed ref target %s: %w", oid, err)
			}
			continue
		}
		snapshot.CommitTrees[oid] = commit.Tree
		validRoots = append(validRoots, oid)
	}
	args := append([]string{"rev-list", "--parents"}, validRoots...)
	out, err := repo.Output(ctx, args...)
	if err != nil {
		return err
	}
	snapshot.KnownCommits = map[string]bool{}
	snapshot.CommitParents = map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		snapshot.KnownCommits[fields[0]] = true
		snapshot.CommitParents[fields[0]] = append([]string(nil), fields[1:]...)
	}
	snapshot.Reachable = map[string]bool{}
	stack := []string{snapshot.MainCommit}
	for len(stack) > 0 {
		last := len(stack) - 1
		oid := stack[last]
		stack = stack[:last]
		if snapshot.Reachable[oid] {
			continue
		}
		snapshot.Reachable[oid] = true
		stack = append(stack, snapshot.CommitParents[oid]...)
	}
	snapshot.MatchingSources = map[string]bool{}
	snapshot.ValidSources = map[string]bool{}
	keys := make([]string, 0, len(sources))
	for key := range sources {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		source := sources[key]
		if !snapshot.CommitKnown(source.Commit) {
			continue
		}
		object, err := batch.Read(source.Commit + ":" + source.Path)
		if err != nil {
			continue
		}
		matches := object.Type == "blob" && object.OID == source.Blob
		snapshot.MatchingSources[key] = matches
		snapshot.ValidSources[key] = matches && snapshot.Reachable[source.Commit]
	}
	return nil
}
