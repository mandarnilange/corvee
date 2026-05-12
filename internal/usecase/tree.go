package usecase

import (
	"context"
	"fmt"
	"sort"

	"github.com/mandarnilange/corvee/internal/domain"
)

// TreeInput is the request payload for Tree.
type TreeInput struct {
	// Root, when non-empty, restricts the tree to the subtree rooted at
	// that ID. Empty returns every project as a root.
	Root string
	// MaxDepth caps the recursion depth (0 = unlimited).
	MaxDepth int
}

// TreeNode is one node of the rendered hierarchy.
type TreeNode struct {
	// Item is the per-node payload.
	Item domain.Item `json:"item"`
	// Children is the sorted list of child subtrees.
	Children []TreeNode `json:"children,omitempty"`
}

// TreeOutput is the response payload for Tree.
type TreeOutput struct {
	// Roots is the top-level node(s) of the requested view. When Root
	// is empty, Roots contains every project; when Root is set, Roots
	// contains a single subtree.
	Roots []TreeNode `json:"roots"`
}

// Tree returns the hierarchical view of the workspace. Implementation
// loads every item up front (Phase 2 scale is bounded by the index
// cache; the optimization to use Indexer arrives in §S07's follow-up).
func Tree(ctx context.Context, d Deps, in TreeInput) (TreeOutput, error) {
	items, err := d.Store.List(ctx, domain.ListFilter{})
	if err != nil {
		return TreeOutput{}, fmt.Errorf("tree: list: %w", err)
	}
	g := domain.NewGraph(items)

	if in.Root != "" {
		root, ok := g.Item(in.Root)
		if !ok {
			return TreeOutput{}, fmt.Errorf("tree: root %q: %w", in.Root, domain.ErrNotFound)
		}
		node := buildTreeNode(g, root, 1, in.MaxDepth)
		return TreeOutput{Roots: []TreeNode{node}}, nil
	}

	rootIDs := g.Roots()
	sort.Strings(rootIDs)
	out := TreeOutput{Roots: make([]TreeNode, 0, len(rootIDs))}
	for _, rootID := range rootIDs {
		root, _ := g.Item(rootID)
		out.Roots = append(out.Roots, buildTreeNode(g, root, 1, in.MaxDepth))
	}
	return out, nil
}

// buildTreeNode recurses depth-first. depth is 1-based; maxDepth==0
// means unlimited.
func buildTreeNode(g domain.Graph, it domain.Item, depth, maxDepth int) TreeNode {
	node := TreeNode{Item: it}
	if maxDepth != 0 && depth >= maxDepth {
		return node
	}
	childIDs := g.Children(it.ID)
	if len(childIDs) == 0 {
		return node
	}
	node.Children = make([]TreeNode, 0, len(childIDs))
	for _, cid := range childIDs {
		child, ok := g.Item(cid)
		if !ok {
			continue
		}
		node.Children = append(node.Children, buildTreeNode(g, child, depth+1, maxDepth))
	}
	return node
}
