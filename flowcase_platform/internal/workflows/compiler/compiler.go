// Package compiler turns a definition into an immutable graph the runtime executes: successor and
// predecessor maps, split/join pairing with expected branch counts, and a topological order.
// Build is tolerant (it skips dangling edges) so the validator can analyse broken drafts.
package compiler

import (
	"slices"

	"github.com/elenio/flowcase/internal/workflows/definition"
)

type Graph struct {
	Def   *definition.Definition
	Index map[string]int // node id -> position in Def.Nodes (first occurrence)
	Succ  map[string][]string
	Pred  map[string][]string
	Start string
	Ends  []string
	// SplitJoin maps a parallel_split to its parallel_join (via the join's split_ref).
	SplitJoin map[string]string
	// JoinExpects is the number of branch tokens a join waits for (= outgoing edges of its split).
	JoinExpects map[string]int
	// Order is a topological order of nodes reachable from start; nil when the graph has a cycle.
	Order []string
}

func Build(d *definition.Definition) *Graph {
	g := &Graph{Def: d, Index: map[string]int{}, Succ: map[string][]string{}, Pred: map[string][]string{},
		SplitJoin: map[string]string{}, JoinExpects: map[string]int{}}
	for i, n := range d.Nodes {
		if _, dup := g.Index[n.ID]; !dup {
			g.Index[n.ID] = i
		}
	}
	for _, n := range d.Nodes {
		if _, seen := g.Succ[n.ID]; seen {
			continue // duplicate ids: keep the first node's edges
		}
		g.Succ[n.ID] = []string{}
		for _, t := range n.Next {
			if _, ok := g.Index[t]; ok && !slices.Contains(g.Succ[n.ID], t) {
				g.Succ[n.ID] = append(g.Succ[n.ID], t)
				g.Pred[t] = append(g.Pred[t], n.ID)
			}
		}
		switch n.Type {
		case definition.Start:
			if g.Start == "" {
				g.Start = n.ID
			}
		case definition.End:
			g.Ends = append(g.Ends, n.ID)
		case definition.ParallelJoin:
			if split := n.ConfigString("split_ref"); split != "" {
				if _, taken := g.SplitJoin[split]; !taken {
					g.SplitJoin[split] = n.ID
				}
			}
		}
	}
	for split, join := range g.SplitJoin {
		g.JoinExpects[join] = len(g.Succ[split])
	}
	g.Order = g.topo()
	return g
}

func (g *Graph) Node(id string) *definition.Node {
	i, ok := g.Index[id]
	if !ok {
		return nil
	}
	return &g.Def.Nodes[i]
}

// Reachable returns every node reachable from the given roots (roots included).
func (g *Graph) Reachable(roots ...string) map[string]bool {
	seen := map[string]bool{}
	stack := slices.Clone(roots)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, g.Succ[id]...)
	}
	return seen
}

// CanReachEnd returns the set of nodes from which some end node is reachable.
func (g *Graph) CanReachEnd() map[string]bool {
	seen := map[string]bool{}
	stack := slices.Clone(g.Ends)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, g.Pred[id]...)
	}
	return seen
}

// Cycle returns the node ids of one cycle reachable from start, or nil.
func (g *Graph) Cycle() []string {
	const (
		white = iota
		grey
		black
	)
	color := map[string]int{}
	var path []string
	var found []string
	var visit func(string) bool
	visit = func(id string) bool {
		color[id] = grey
		path = append(path, id)
		for _, s := range g.Succ[id] {
			switch color[s] {
			case grey:
				i := slices.Index(path, s)
				found = slices.Clone(path[i:])
				return true
			case white:
				if visit(s) {
					return true
				}
			}
		}
		path = path[:len(path)-1]
		color[id] = black
		return false
	}
	for _, n := range g.Def.Nodes {
		if color[n.ID] == white && visit(n.ID) {
			return found
		}
	}
	return nil
}

func (g *Graph) topo() []string {
	if g.Start == "" {
		return nil
	}
	reach := g.Reachable(g.Start)
	indeg := map[string]int{}
	for id := range reach {
		for _, p := range g.Pred[id] {
			if reach[p] {
				indeg[id]++
			}
		}
	}
	queue := []string{g.Start}
	var order []string
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		order = append(order, id)
		for _, s := range g.Succ[id] {
			indeg[s]--
			if indeg[s] == 0 {
				queue = append(queue, s)
			}
		}
	}
	if len(order) != len(reach) {
		return nil
	}
	return order
}

// Region returns the nodes strictly between a split and its join: everything reachable from the
// split's branches without passing through the join.
func (g *Graph) Region(split, join string) map[string]bool {
	seen := map[string]bool{}
	stack := slices.Clone(g.Succ[split])
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == join || seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, g.Succ[id]...)
	}
	return seen
}

// AllPathsReach reports whether every path starting at from reaches target before any end node.
// It assumes an acyclic graph.
func (g *Graph) AllPathsReach(from, target string) bool {
	memo := map[string]bool{}
	var walk func(string) bool
	walk = func(id string) bool {
		if id == target {
			return true
		}
		if v, ok := memo[id]; ok {
			return v
		}
		memo[id] = false
		succ := g.Succ[id]
		ok := len(succ) > 0
		for _, s := range succ {
			if !walk(s) {
				ok = false
				break
			}
		}
		memo[id] = ok
		return ok
	}
	return walk(from)
}
