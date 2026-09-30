package loom

import (
	"strings"
	"text/template"
	"text/template/parse"
)

// selfRefs returns the document paths that the template reads under the self
// key, with the self key stripped. A bare reference to the whole document
// (".self") is returned as "".
//
// References are found by walking the parsed template for field nodes rooted at
// the self key. This also captures the base of with/range blocks (for example
// "{{ with .self.spec }}...{{ end }}"), which is sufficient: depending on that
// base orders the whole subtree before this field, so relative fields inside the
// block observe rendered values.
func selfRefs(t *template.Template, selfKey string) []string {
	seen := map[string]struct{}{}
	var refs []string
	add := func(p string) {
		if _, ok := seen[p]; !ok {
			seen[p] = struct{}{}
			refs = append(refs, p)
		}
	}

	var walk func(n parse.Node)
	walk = func(n parse.Node) {
		if n == nil {
			return
		}
		switch node := n.(type) {
		case *parse.ListNode:
			if node == nil {
				return
			}
			for _, c := range node.Nodes {
				walk(c)
			}
		case *parse.ActionNode:
			walk(node.Pipe)
		case *parse.PipeNode:
			if node == nil {
				return
			}
			for _, cmd := range node.Cmds {
				walk(cmd)
			}
		case *parse.CommandNode:
			for _, a := range node.Args {
				walk(a)
			}
		case *parse.FieldNode:
			if len(node.Ident) >= 1 && node.Ident[0] == selfKey {
				add(strings.Join(node.Ident[1:], "."))
			}
		case *parse.ChainNode:
			walk(node.Node)
		case *parse.IfNode:
			walk(node.Pipe)
			walk(node.List)
			walk(node.ElseList)
		case *parse.RangeNode:
			walk(node.Pipe)
			walk(node.List)
			walk(node.ElseList)
		case *parse.WithNode:
			walk(node.Pipe)
			walk(node.List)
			walk(node.ElseList)
		case *parse.TemplateNode:
			walk(node.Pipe)
		}
	}

	if t.Tree != nil {
		walk(t.Root)
	}
	return refs
}

// buildDeps resolves each leaf's self-reference paths to the leaves it depends
// on. A reference to a path depends on every leaf at that path or within its
// subtree (and on any ancestor leaf the path reads into). A bare self reference
// depends on all other leaves.
func buildDeps(leaves []*leaf) {
	for _, x := range leaves {
		seen := map[*leaf]struct{}{}
		for _, ref := range x.refs {
			for _, y := range leaves {
				if y == x {
					continue
				}
				if ref == "" || pathMatches(ref, y.path) {
					if _, ok := seen[y]; !ok {
						seen[y] = struct{}{}
						x.deps = append(x.deps, y)
					}
				}
			}
		}
	}
}

// pathMatches reports whether a reference to ref should depend on a leaf at
// leafPath: they are equal, ref is an ancestor of the leaf, or the leaf is an
// ancestor the reference reads into.
func pathMatches(ref, leafPath string) bool {
	if ref == leafPath {
		return true
	}
	if strings.HasPrefix(leafPath, ref+".") || strings.HasPrefix(leafPath, ref+"[") {
		return true
	}
	if strings.HasPrefix(ref, leafPath+".") || strings.HasPrefix(ref, leafPath+"[") {
		return true
	}
	return false
}

// topoSort returns the leaves ordered so that every leaf appears after its
// dependencies. It returns a *CycleError (which is ErrReferenceCycle) if the
// dependency graph contains a cycle.
func topoSort(leaves []*leaf) ([]*leaf, error) {
	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := make(map[*leaf]int, len(leaves))
	var order, stack []*leaf

	var visit func(l *leaf) error
	visit = func(l *leaf) error {
		switch color[l] {
		case black:
			return nil
		case gray:
			idx := 0
			for i, s := range stack {
				if s == l {
					idx = i
					break
				}
			}
			cycle := make([]string, 0, len(stack)-idx+1)
			for _, s := range stack[idx:] {
				cycle = append(cycle, s.path)
			}
			cycle = append(cycle, l.path)
			return &CycleError{Fields: cycle}
		}
		color[l] = gray
		stack = append(stack, l)
		for _, d := range l.deps {
			if err := visit(d); err != nil {
				return err
			}
		}
		stack = stack[:len(stack)-1]
		color[l] = black
		order = append(order, l)
		return nil
	}

	for _, l := range leaves {
		if err := visit(l); err != nil {
			return nil, err
		}
	}
	return order, nil
}
