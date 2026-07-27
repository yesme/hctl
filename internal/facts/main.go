package facts

import (
	"context"
	"fmt"

	"github.com/yesme/hctl/internal/gitx"
)

// readMainHistory walks only the first-parent integration line while sharing a
// single cat-file process. Side parents are retained for non-squash detection.
func readMainHistory(ctx context.Context, repo *gitx.Repo, tip string) ([]MainCommit, error) {
	if tip == "" {
		return nil, nil
	}
	batch, err := repo.NewBatch(ctx)
	if err != nil {
		return nil, err
	}
	defer batch.Close()
	var history []MainCommit
	seen := map[string]bool{}
	oid := tip
	for oid != "" {
		if seen[oid] {
			return nil, fmt.Errorf("cycle in main history at %s", oid)
		}
		seen[oid] = true
		object, err := batch.Read(oid)
		if err != nil {
			return nil, err
		}
		if object.Type != "commit" {
			return nil, fmt.Errorf("main object %s is %s, not commit", oid, object.Type)
		}
		commit, err := gitx.ParseCommit(object.Data)
		if err != nil {
			return nil, err
		}
		history = append(history, MainCommit{
			OID: oid, Tree: commit.Tree, Parents: append([]string(nil), commit.Parents...), Message: commit.Message,
		})
		if len(commit.Parents) == 0 {
			break
		}
		oid = commit.Parents[0]
	}
	return history, nil
}
