package ops

// Author: Charles Randolph
// Function:
//   Ops (short for "operations") provides
//   1. Complex graphing operations

import (

	// Standard packages
	"fmt"
	"errors"

	// Custom packages
	"graph"
)


/*
 *******************************************************************************
 *                              Type Definitions                               *
 *******************************************************************************
*/


type Edge struct {
	Tag       int          // Sequence identifier
	Num       int          // Sequence edge number
	Color     string       // Color to use with edge
}


/*
 *******************************************************************************
 *                         Public Function Definitions                         *
 *******************************************************************************
*/


// Returns true if a given edge is equal to another
func (e *Edge) Equals (other *Edge) bool {
	return (e.Tag == other.Tag) && (e.Num == other.Num)
}

// Returns the larger of two integer numbers (silly but no default)
func Max (a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Returns the number of nodes in a slice of chains
func NodeCount (chains []int) int {
	sum := 0
	for _, c := range chains {
		sum += c
	}
	return sum
}

// Returns a string describing a path
func Path2String (path []int) string {
	s := "{"
	for i, n := range path {
		s += fmt.Sprintf("%d", n)
		if i < (len(path)-1) {
			s += ","
		}
	}
	return s + "}"
}

// Returns a string describing an edge. To be used with graph.Map
func Show (x interface{}) string {
	es := x.([]*Edge)
	if len(es) == 0 {
		return " _ "
	}
	s := fmt.Sprintf("(%d", es[0].Tag)
	for i := 1; i < len(es); i++ {
		s += fmt.Sprintf(",%d", es[i].Tag)
	}
	return s + ")"
}

// Returns a slice of edges from the given index in the graph. Panics on error
func EdgesAt (row, col int, g *graph.Graph) []*Edge {
	val, err := g.Get(row, col)
	if nil != err {
		panic(err)
	}
	if nil == val {
		return []*Edge{}
	}
	return val.([]*Edge)
}

// Returns all rows on which chains start, given ordered chain lengths
func StartingRows (chains []int) []int {
	starting_rows := []int{}
	for i, sum := 0, 0; i < len(chains); i++ {
		starting_rows = append(starting_rows, sum)
		sum += chains[i]
	}
	return starting_rows	
}

// Returns the chain to which the given row belongs
func ChainForRow (row int, chains []int) int {
	i, sum := 0, 0
	for i = 0; i < len(chains); i++ {
		if (row >= sum) && (row < (sum + chains[i])) {
			break
		} else {
			sum += chains[i]
		}
	}
	return i	
}

// Returns true if an edge exists between (a,b) in the graph (g)
func EdgeBetween (a, b int, g *graph.Graph) bool {

	// Closure: Returns true if x has a directed edge to y
	has_outgoing_edge := func (x, y int, g *graph.Graph) bool {
		edges := EdgesAt(x,y,g)
		return (len(edges) > 0)
	}

	return (has_outgoing_edge(a, b, g) || has_outgoing_edge(b, a, g))	
}

// Returns true if the given row has no incoming or outgoing edges
func Disconnected (row int, g *graph.Graph) bool {

	// Check row (outgoing)
	for i := 0; i < g.Len(); i++ {
		edges := EdgesAt(row, i, g)
		if (len(edges) > 0) {
			return false
		}
	}

	// Check column (incoming)
	for i := 0; i < g.Len(); i++ {
		edges := EdgesAt(i, row, g)
		if (len(edges) > 0) {
			return false
		}
	}

	return true	
}

// Returns (column, *edge) at which a given edge is found for a row. Else (-1, nil)
func ColumnForEdge (tag, num, row int, g *graph.Graph) (int, *Edge) {
	var edge *Edge = nil
	var col int    = -1

	for i := 0; i < g.Len(); i++ {
		edges := EdgesAt(row, i, g)
		if len(edges) == 0 {
			continue
		}
		for _, e := range edges {
			if e.Tag == tag && e.Num == num {
				edge = e
				col = i
				break
			}
		}
	}
	return col, edge	
}

// Returns a sequence of visited nodes (rows) for a given chain
func PathForChain (chain_id int, chains []int, g *graph.Graph) []int {
	row, col := -1, -1
	path     := []int{}

	// Chains always start in start-rows. Obtain all starting rows
	start_rows := StartingRows(chains)

	// Chains with a single element may not merge. Return it as the path
	if chains[chain_id] == 1 {
		return []int{start_rows[chain_id]}
	}

	// Locate the starting row
	for _, r := range start_rows {
		if c, _ := ColumnForEdge(chain_id, 0, r, g); c != -1 {
			row = r
			col = c
			break
		}
	}

	// Panic if the start row wasn't found
	if col == -1 {
		reason := fmt.Sprintf("Unable to find starting edge for chain %d", chain_id)
		panic(errors.New(reason))
	} else {
		path = append(path, row)
	}

	// Visit the row of the column, repeat until no more edges found
	for col != -1 {

		// Next row to visit is that of the column
		row = col

		// Push to path
		path = append(path, row)

		// Find successor
		col, _ = ColumnForEdge(chain_id, len(path) - 1, row, g)
	}

	return path
}

// Adjusts edge identified by (tag,num) at (from, to) to (from, dest)
func RewireTo (from, to, tag, num, dest int, g *graph.Graph) error {
	var edge *Edge = nil

	// Remove existing edge at position (from, to)
	edges := EdgesAt(from, to, g)
	for i, e := range edges {
		if (e.Tag == tag) && (e.Num == num) {
			edge = e
			g.Set(from, to, append(edges[:i], edges[i+1:]...))
			break
		}
	}

	// Verify edge was found and removed
	if nil == edge {
		reason := fmt.Sprintf("Edge ID(%d,%d) not found at (%d,%d)!",
			tag, num, from, to)
		return errors.New(reason)
	}

	// Insert edge at destination column
	edges = EdgesAt(from, dest, g)
	edges = append(edges, edge)
	return g.Set(from, dest, edges)
}

// Extends given NxN graph to (N+1)x(N+1) graph, and returns new length
func ExtendGraphByOne (g *graph.Graph) int {
	n := g.Len()
	var expanded_graph graph.Graph = make([][]interface{}, n+1)
	for i := 0; i < (n+1); i++ {
		expanded_graph[i] = make([]interface{}, n+1)
	}
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			expanded_graph[i][j] = (*g)[i][j]
		}
	}
	(*g) = expanded_graph

	return (n+1)	
}

// Adds an edge to the given graph
func Wire (from, to, tag, num int, color string, g *graph.Graph) error {
	edges := EdgesAt(from, to, g)
	edges = append(edges, &Edge{Tag: tag, Num: num, Color: color})
	return g.Set(from, to, edges)
}

// Merges row 'from' into row 'to' in graph 'g'
func Merge (from, to int, g *graph.Graph) error {
	var err error = nil

	// All contents in row 'from' should be moved to row 'to'
	for i := 0; i < g.Len(); i++ {
		src_edges, dst_edges := EdgesAt(from, i, g), EdgesAt(to, i, g)
		if len(src_edges) == 0 {
			continue
		}
		err = g.Set(to, i, append(src_edges, dst_edges...))
		if nil != err {
			return err
		}
		err = g.Set(from, i, nil)
		if nil != err {
			return err
		}
	}

	// All contents in column 'from' should be moved to column 'to'
	for i := 0; i < g.Len(); i++ {
		src_edges, dst_edges := EdgesAt(i, from, g), EdgesAt(i, to, g)
		if len(src_edges) == 0 {
			continue
		}
		err = g.Set(i, to, append(src_edges, dst_edges...))
		if nil != err {
			return err
		}
		err = g.Set(i, from, nil)
		if nil != err {
			return err
		}
	}

	return nil
}

// Returns a new graph sized for given chains. With chain edge colors
func InitGraph (chains []int, chain_colors []string) *graph.Graph {

	// Compute number of rows required
	n := NodeCount(chains)

	// Initialize graph
	var g graph.Graph = make([][]interface{}, n)

	// Setup columns and rows
	for row := 0; row < n; row++ {
		g[row] = make([]interface{}, n)
		for col := 0; col < n; col++ {
			g[row][col] = []*Edge{}
		}
	}

	// Setup all chains
	for i, offset := 0,0; i < len(chains); i++ {
		for j := 0; j < (chains[i] - 1); j++ {
			edge := Edge{Tag: i, Num: j, Color: chain_colors[i]}
			g.Set(offset+j, offset+j+1, []*Edge{&edge})
		}
		offset += chains[i]
	}

	return &g
}