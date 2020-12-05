package main

import (
	"os"
	"fmt"
	"errors"
	"text/template"
	"io/ioutil"
	"bufio"
	"math"
	"math/rand"
	"runtime"
	"encoding/json"
	"os/exec"

	// Custom packages
	"temporal"
	"graph"
	"set"
	"benchmark"
	"colors"
	"rosgraph/gen"

	// Third party packages
	"github.com/gookit/color"
)

/*
 *******************************************************************************
 *                        Input/Output Type Definitions                        *
 *******************************************************************************
*/

type Config struct {
	Path              string
	Chain_count       int
	Chain_avg_len     int
	Chain_merge_p     float64
	Chain_sync_p      float64
	Chain_variance    float64
	Util_total        float64
	Min_period_ns     int
	Max_period_ns     int
} 

/*
 *******************************************************************************
 *                           Graph Type Definitions                            *
 *******************************************************************************
*/

type Edge struct {
	Tag       int          // Sequence identifier
	Num       int          // Sequence edge number
	Color     string       // Color to use with edge
}

/*
 *******************************************************************************
 *                          Graphviz Type Definitions                          *
 *******************************************************************************
*/

type Link struct {
	From      int                 // Source node
	To        int                 // Destination node
	Color     string              // Link color
	Label     string              // Link label
}

type Node struct {
	Id        int                 // Node ID
	Label     string              // Label for the node
	Style     string              // Border style
	Fill      string              // Color indicating fill of the node
	Shape     string              // Shape of the node
}

type Graphviz_graph struct {
	Nodes     []Node              // Nested clusters
	Links     []Link              // Slice of links
}

type Graphviz_application struct {
	App       *gen.Application    // Application structure
	Links     []Link              // Slice of links
}

/*
 *******************************************************************************
 *                          Template Type Definitions                          *
 *******************************************************************************
*/

type ROS_Executor struct {
	Includes     []string         // Include directives for C++ program
	MsgType      string           // Program message type
	FilterPolicy string           // Policy for message filters
	PPE          bool             // Whether to use PPE types and semantics
	Executor     gen.Executor     // The executor to parse
}

/*
 *******************************************************************************
 *                           Format Output Functions                           *
 *******************************************************************************
*/

func check (err error, s string, args ...interface{}) func() {
	if nil != err {
		return func () {
			color.Error.Printf("Fault: %s\nCause: %s\n", fmt.Sprintf(s, args...), err.Error())
			panic(err.Error())
		}
	} else {
		return func (){}
	}
}

func warn (s string, args ...interface{}) {
	color.Warn.Printf("Warning: %s\n", fmt.Sprintf(s, args...))
}

func show_config (cfg Config) {
	color.Black.Printf("Path:           "); color.Magenta.Printf("%s\n", cfg.Path)
	color.Black.Printf("Chain_count:    "); color.Magenta.Printf("%d\n", cfg.Chain_count)
	color.Black.Printf("Chain_avg_len:  "); color.Magenta.Printf("%d\n", cfg.Chain_avg_len)
	color.Black.Printf("Chain_merge_p:  "); color.Magenta.Printf("%.2f\n", cfg.Chain_merge_p)
	color.Black.Printf("Chain_sync_p:   "); color.Magenta.Printf("%.2f\n", cfg.Chain_sync_p)
	color.Black.Printf("Chain_variance: "); color.Magenta.Printf("%.2f\n", cfg.Chain_variance)
	color.Black.Printf("Util_total:     "); color.Magenta.Printf("%.2f\n", cfg.Util_total)
	color.Black.Printf("Min_period_ns:  "); color.Magenta.Printf("%d\n", cfg.Min_period_ns)
	color.Black.Printf("Max_period_ns:  "); color.Magenta.Printf("%d\n", cfg.Max_period_ns)
}

/*
 *******************************************************************************
 *                          Graph Operation Functions                          *
 *******************************************************************************
*/

// Returns a string describing an edge. Used with graph.Map
func show (x interface{}) string {
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

// Extracts a slice of edges from the given index in the graph. Panics on err
func edgesAt (row, col int, g *graph.Graph) []*Edge {
	val, err := g.Get(row, col)
	if nil != err {
		panic(err)
	}
	if nil == val {
		return []*Edge{}
	}
	return val.([]*Edge)
}

// Returns starting row of all chains, given an ordeMagenta slice of chain lengths
func get_start_rows (chains []int) []int {
	starting_rows := []int{}
	for i, sum := 0, 0; i < len(chains); i++ {
		starting_rows = append(starting_rows, sum)
		sum += chains[i]
	}
	return starting_rows
}

// Returns the chain to which the given row belongs to (given all chains)
func get_row_chain (row int, chains []int) int {
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

// Returns true if 'from' has an edge to 'to'
func edge_between (a, b int, g *graph.Graph) bool {

	// Closure: Returns true if x has a directed edge to y
	has_outgoing_edge := func (x, y int, g *graph.Graph) bool {
		edges := edgesAt(x,y,g)
		return (len(edges) > 0)
	}

	return (has_outgoing_edge(a, b, g) || has_outgoing_edge(b, a, g))
}

// Returns true if 'row' has no incoming or outgoing edges
func is_isolated (row int, g *graph.Graph) bool {

	// Check row (outgoing)
	for i := 0; i < g.Len(); i++ {
		edges := edgesAt(row, i, g)
		if (len(edges) > 0) {
			return false
		}
	}

	// Check column (incoming)
	for i := 0; i < g.Len(); i++ {
		edges := edgesAt(i, row, g)
		if (len(edges) > 0) {
			return false
		}
	}

	return true
}

// Returns (column,*edge) at which a given edge is found for a row. Else -1, nil
func col_for_edge (tag, num, row int, g *graph.Graph) (int, *Edge) {
	var edge *Edge = nil
	var col int    = -1

	for i := 0; i < g.Len(); i++ {
		edges := edgesAt(row, i, g)
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

// Returns sequence of nodes (rows) visited by a path (chain)
func get_path (id int, chains []int, g *graph.Graph) []int {
	var row, col int = -1, -1
	var path []int = []int{}

	// Chains may only merge between start nodes. Starting edge is in this row
	start_rows := get_start_rows(chains)

	// If the chain has only one element - it cannot be merged so return row
	if chains[id] == 1 {
		return []int{start_rows[id]}
	}

	// Locate row on which starting edge resides
	for i := 0; i < len(start_rows); i++ {
		if c, _ := col_for_edge(id, 0, start_rows[i], g); c != -1 {
			row = start_rows[i]
			col = c
			break
		}
	}

	// If starting edge not found - panic
	if col == -1 {
		panic(errors.New(fmt.Sprintf("Unable to find starting edge for chain %d", id)))
	} else {
		path = append(path, row)
	}

	for col != -1 {

		// Next row to visit is that along the column
		row = col

		// Push this next row to the path
		path = append(path, row)

		// Find and set next column
		col, _ = col_for_edge(id, len(path) - 1, row, g)
	}

	// Assert: Number of nodes (doesn't work if the graph is extended)
	// if len(path) != chains[id] {
	// 	panic(errors.New(fmt.Sprintf("Node count inconsistent for chain %d", id)))
	// }

	return path
}

// Removes edge with tag and num at row 'from', col 'to', and shifts it to col 'dest'
func set_edge_destination (from, to, tag, num, dest int, g *graph.Graph) {
	var edge *Edge = nil

	// The edge is in row 'from', col 'to'. It should be removed from there first
	edges := edgesAt(from, to, g)
	for i, e := range edges {
		if (e.Tag == tag) && (e.Num == num) {
			edge = e
			g.Set(from, to, append(edges[:i], edges[i+1:]...))
			break
		}
	}

	// Assert that an edge was found, as it is expected
	if nil == edge {
		check(errors.New("Edge not found!"), "Edge expected at (%d,%d) from chain %d\n",
			from, to, tag)()
	}

	// Insert the current edge at the destination column
	edges = edgesAt(from, dest, g)
	edges = append(edges, edge)
	err := g.Set(from, dest, edges)
	check(err, fmt.Sprintf("Couldn't set edges at (%d,%d) in graph!", from, dest))()
}

// Extends the given NxN graph to an (N+1)x(N+1) graph, returns new length
func add_node (g *graph.Graph) int {
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

// Adds an edge to the graph
func add_edge (from, to, tag, num int, color string, g *graph.Graph) error {
	e := &Edge{Tag: tag, Num: num, Color: color}
	edges := edgesAt(from, to, g)
	edges = append(edges, e)
	return g.Set(from, to, edges)
}

/*
 *******************************************************************************
 *                               Graph Functions                               *
 *******************************************************************************
*/

// Initializes a new graph using given chains. Edges for each chain assigned a color
func init_graph (chains []int, palette []string) *graph.Graph {
	rows := 0;

	// RequiMagenta rows is sum of chain lengths
	for _, n := range chains {
		rows += n
	}

	// Initialize the graph
	var g graph.Graph = make([][]interface{}, rows)

	// Create columns and empty rows (NxN)
	for i := 0; i < rows; i++ {
		g[i] = make([]interface{}, rows)
		for j := 0; j < rows; j++ {
			g[i][j] = []*Edge{}
		}
	}

	// Initialize all chains
	for i, offset := 0, 0; i < len(chains); i++ {
		for j := 0; j < (chains[i] - 1); j++ {
			g.Set(offset+j, offset+j+1, []*Edge{&Edge{Tag: i, Num: j, Color: palette[i]}})
		}
		offset += chains[i]
	}

	return &g
}

// Merges element on row 'from' into element on row 'to' in graph 'g'
func merge (from, to int, g *graph.Graph) {

	// All slices in row 'from' should be moved to row 'to'
	for i := 0; i < g.Len(); i++ {
		src_edges, dst_edges := edgesAt(from, i, g), edgesAt(to, i, g)
		if len(src_edges) == 0 {
			continue
		}
		g.Set(to, i, append(src_edges, dst_edges...))
		g.Set(from, i, nil)
	}

	// All slices in column 'from' should be moved to column 'to'
	for i := 0; i < g.Len(); i++ {
		src_edges, dst_edges := edgesAt(i, from, g), edgesAt(i, to, g)
		if len(src_edges) == 0 {
			continue
		}
		g.Set(i, to, append(src_edges, dst_edges...))
		g.Set(i, from, nil)
	}
}

// Returns true if a merge is allowed, per the merge rules
func can_merge (from, to int, chains []int, g *graph.Graph) bool {
	var map_chain_to_path map[int]([]int) = make(map[int]([]int))

	// Closure: Returns true if slice contains value
	contains := func (x int, xs []int) bool {
		for _, y := range xs {
			if x == y {
				return true
			}
		}
		return false
	}

	// Closure: Returns true if the given path has a cycle
	cycle_in_paths := func (chain int, path []int) bool {
		old_path := map_chain_to_path[chain]
		map_chain_to_path[chain] = path
		cycle := false
		for _, p := range map_chain_to_path {
			visited := make(map[int]int)
			for i := 0; i < len(p); i++ {
				if _, exists := visited[p[i]]; exists {
					cycle = true
					break 
				} else {
					visited[p[i]] = 1
				}
			}
		}
		map_chain_to_path[chain] = old_path
		return cycle
	}

	// Closure: Replaces occurrences of 'match' with 'replacement' in given path, returns new path
	replace_in_path := func (match, replacement int, path []int) []int {
		clone := []int{}
		for i := 0; i < len(path); i++ {
			if path[i] == match {
				clone = append(clone, replacement)
			} else {
				clone = append(clone, path[i])
			}
		}
		return clone
	}

	// Closure: Returns true if every chain has at least one unique node
	unique_paths := func (from, to int) bool {
		visited := make([]int, g.Len())

		for _, path := range map_chain_to_path {
			for _, node := range path {
				if node == from {
					visited[to]++
				} else {
					visited[node]++
				}
			}
		}
		for _, path := range map_chain_to_path {
			unique_nodes := 0
			for _, node := range path {
				if visited[node] == 1 {
					unique_nodes++
				}
			}
			if unique_nodes == 0 {
				return false
			}
		}
		return true
	}

	ppath := func (path []int) {
		fmt.Printf("{ ")
		for i := 0; i < len(path); i++ {
			fmt.Printf("%d ", path[i])
		}
		fmt.Printf(" }\n")
	}

	// Compute all current paths for the chains
	for i := 0; i < len(chains); i++ {
		map_chain_to_path[i] = get_path(i, chains, g)
		ppath(map_chain_to_path[i])
	}

	// Condition: Starting elements are special and cannot merge
	// Solution:  If 'from' and 'to' are not the same type of element - return false
	start_rows := get_start_rows(chains)
	if contains(from, start_rows) != contains(to, start_rows) {
		fmt.Printf("Cannot merge %d and %d, as nodes are of incompatible types!\n", from, to)
		return false
	}

	// Condition: The length of a path must not be shortened during a merge
	// Solution: If 'to' and 'from' have any direct edges between one another - return false
	if edge_between(from, to, g) {
		fmt.Printf("Cannot merge %d and %d, as there is a direct edge between them!\n", from, to)
		return false
	}

	// Condition: A path cannot have loops
	// Solution:  Replace every path containing 'from' with 'to'. Then check for a loop
	for chain_id, chain_path := range map_chain_to_path {
		if cycle_in_paths(chain_id, replace_in_path(from, to, chain_path)) {
			fmt.Printf("Cannot merge %d->%d as a cycle is formed in chain %d\n", from, to, chain_id)
			return false
		}
	}

	// Condition: Chains with only one callback cannot be merged (shortcut for next rule)
	// Solution: If either node belongs to chain of length 1 - return false
	to_chain_id, from_chain_id := get_row_chain(to, chains), get_row_chain(from, chains)
	if chains[to_chain_id] == 1 || chains[from_chain_id] == 1 {
		fmt.Printf("Cannot merge %d and %d, as one of the chains will not be distinguishable (1)\n", from, to)
	}

	// Condition: Don't allow chains to be completely merged with one another
	// Solution: If merging the node means there remains no unique node between the 
	//           chain being merged into, or the chain merging, from all other other 
	//           chains - then disallow the merge
	if unique_paths(from, to) == false {
		fmt.Printf("Cannot merge %d and %d, as the chains will not be distinguishable (2.1)\n", from, to)
		return false		
	}

	// Condition: A merged element may not be connected to again (no longer 'exists')
	if is_isolated(from, g) || is_isolated(to, g) {
		fmt.Printf("Cannot merge %d and %d, as one of them is isolated (already merged elsewhere)\n", from, to)
		return false
	}

	fmt.Printf("Merge (%d,%d) allowed!\n", from, to)
	return true
}

/*
 *******************************************************************************
 *                        Template Generation Functions                        *
 *******************************************************************************
*/

// Generates a file given a data structure, path to template, and output file path
func generate_from_template (data interface{}, in_path, out_path string) error {
	var t *template.Template = nil
	var err error = nil
	var out_file *os.File = nil
	var template_file []byte = []byte{}

	// check: valid input
	if nil == data {
		return errors.New("bad argument: null pointer")
	}
	// Yes, you can use == with string comparisons in go
	if in_path == out_path {
		return errors.New("input file (template) cannot be same as output file")
	}

	// Create the output file
	out_file, err = os.Create(out_path)
	if nil != err {
		return errors.New("unable to create output file (" + out_path + "): " + err.Error())
	}
	defer out_file.Close()

	// Open the template file
	template_file, err = ioutil.ReadFile(in_path)
	if nil != err {
		return errors.New("unable to read input file (" + in_path + "): " + err.Error())
	}
	if template_file == nil {
		panic(errors.New("Nil pointer to read file"))
	}

	t, err = template.New("Unnamed").Parse(string(template_file))
	fmt.Println("parsed!")
	if nil != err {
		return errors.New("unable to parse the template: " + err.Error())
	}

	// Create buffeMagenta writer
	writer := bufio.NewWriter(out_file)
	defer writer.Flush()

	// Execute template
	err = t.Execute(writer, data)
	if nil != err {
		return errors.New("error executing template: " + err.Error())
	}

	return nil
}

/*
 *******************************************************************************
 *                             Graphviz Functions                              *
 *******************************************************************************
*/

// Converts internal graph representation to graphviz application data structure
func application_to_graphviz (a *gen.Application, g *graph.Graph) (Graphviz_application, error) {
	links := []Link{}

	// Create all links
	for i := 0; i < g.Len(); i++ {
		for j := 0; j < g.Len(); j++ {
			edges := edgesAt(i, j, g)
			for _, e := range edges {
				label := fmt.Sprintf("%d.%d", e.Tag, e.Num)
				links = append(links, Link{From: i, To: j, Color: e.Color, Label: label})
			}
		}
	}

	return Graphviz_application{App: a, Links: links}, nil
}

// Converts internal graph representation to graphviz data structure
func graph_to_graphviz (chains []int, node_wcet_map map[int]float64, node_prio_map map[int]int, g *graph.Graph) (Graphviz_graph, error) {
	nodes := []Node{}
	links := []Link{}

	// Closure: Returns true if the given node is connected in the graph
	is_connected := func (row int, g *graph.Graph) bool {
		for i := 0; i < g.Len(); i++ {
			row_edges, col_edges := edgesAt(row, i, g), edgesAt(i, row, g) 
			if (len(row_edges) + len(col_edges)) > 0 {
				return true
			}
		}
		return false
	}

	// Closure: Returns true if the given chain has a length of one
	length_one_chain := func (row int, chains []int) bool {
		return chains[get_row_chain(row, chains)] == 1
	}

	// Closure: Returns the number of nodes belonging to chains
	get_chain_node_count := func (chains []int) int {
		sum := 0
		for i := 0; i < len(chains); i++ {
			sum += chains[i]
		}
		return sum
	}

	// Obtain the number of nodes that belong to chains
	n_chain_nodes := get_chain_node_count(chains)

	// Create all nodes (but only if connected or chain has length 1)
	for i := 0; i < g.Len(); i++ {
		if is_connected(i, g) || length_one_chain(i, chains) {

			// It's a chain node if below the original graph node count
			if i < n_chain_nodes {
				label := fmt.Sprintf("N%d\n(wcet=%.2f)\nprio=%d", i, node_wcet_map[i], node_prio_map[i])
				nodes = append(nodes, Node{Id: i, Label: label, Style: "filled", Fill: "#FFFFFF", Shape: "circle"})				
			} else {
				label := fmt.Sprintf("N%d\n(SYNC)", i)
				nodes = append(nodes, Node{Id: i, Label: label, Style: "filled", Fill: "#FFE74C", Shape: "diamond"})
			}
		
		}
	}

	// Create all links
	for i := 0; i < g.Len(); i++ {
		for j := 0; j < g.Len(); j++ {
			edges := edgesAt(i, j, g)
			for _, e := range edges {
				label := fmt.Sprintf("%d.%d", e.Tag, e.Num)
				links = append(links, Link{From: i, To: j, Color: e.Color, Label: label})
			}
		}
	}

	return Graphviz_graph{Nodes: nodes, Links: links}, nil
}

/*
 *******************************************************************************
 *                       Adding: Synchronization points                        *
 *******************************************************************************
*/

// A container holding an edge outside of the graph context (with from->to info)
type ExtendedEdge struct {
	From   int
	To     int
	Edge   *Edge
}

// Creates an extended version of the supplied graph by adding synchronization points
func add_synchronization_nodes (chain_sync_p float64, chains []int, g *graph.Graph) *graph.Graph {
	map_chain_to_path := make(map[int]([]int))
	node_edge_map     := make(map[int]([]ExtendedEdge))

	// Closure: Inserts a link into the given path
	insert_in_path := func (after_node, new_node, tag int) {
		path := map_chain_to_path[tag]
		new_path := []int{}
		for i := 0; i < len(path); i++ {
			new_path = append(new_path, path[i])
			if path[i] == after_node {
				new_path = append(new_path, new_node)
			}
		}
		map_chain_to_path[tag] = new_path
	}

	// Compute all current paths for the chains
	for i := 0; i < len(chains); i++ {
		map_chain_to_path[i] = get_path(i, chains, g)
	}

	// Locate all nodes with two or more incoming edges
	for i := 0; i < g.Len(); i++ {

		// Check column for incoming edges
		for j := 0; j < g.Len(); j++ {

			// Extract all incoming edges along column i
			edges := edgesAt(j, i, g)

			// If there are no edges, move on
			if len(edges) == 0 {
				continue
			}

			// Build the minimal edge slice
			extended_edges := []ExtendedEdge{}
			for _, e := range edges {
				extended_edges = append(extended_edges, ExtendedEdge{From: j, To: i, Edge: e})
			}

			// Otherwise append them to the existing slice
			if slice := node_edge_map[i]; slice != nil {
				node_edge_map[i] = append(slice, extended_edges...)
			} else {
				node_edge_map[i] = extended_edges
			}
		} 
	}

	// Perform a random merge between two of the pairs
	for node, es := range node_edge_map {
		fmt.Printf("Node %d has %d incoming edges\n", node, len(es))

		// Compute the number of attempts possible
		for len(es) >= 2 {
			n := len(es)
			attempts := (n * (n - 1)) / 2

			// Assume no sync was made
			sync := false

			// Attempt 
			for i := 0; i < attempts; i++ {
				var err error = nil

				// Continue if inverse P 
				if (rand.Float64() >= chain_sync_p) {
					continue
				}

				// Otherwise shuffle
				rand.Shuffle(n, func(x, y int){ es[x], es[y] = es[y], es[x] })

				// Pick first two
				a, b := es[0], es[1]

				// Issue a directive
				fmt.Printf("Will be placing a sync node between %d-[%d]->%d, and %d-[%d]->%d\n",
					a.From, a.Edge.Tag, a.To, b.From, b.Edge.Tag, b.To)
				fmt.Printf("%s", g.String(show))

				// Expand the graph with a new node
				n := add_node(g)

				fmt.Printf("After:\n%s", g.String(show))

				// Update the path with the new node
				insert_in_path(a.From, (n-1), a.Edge.Tag)
				insert_in_path(b.From, (n-1), b.Edge.Tag)

				// Move edges to new node
				set_edge_destination(a.From, a.To, a.Edge.Tag, a.Edge.Num, (n-1), g)
				set_edge_destination(b.From, b.To, b.Edge.Tag, b.Edge.Num, (n-1), g)

				fmt.Printf("Set destination ...\n%s", g.String(show))

				// Wire node to destination
				err = add_edge((n-1), a.To, a.Edge.Tag, a.Edge.Num + 1, a.Edge.Color, g)
				check(err, "Unable to add edge!")()
				err = add_edge((n-1), b.To, b.Edge.Tag, b.Edge.Num + 1, b.Edge.Color, g)
				check(err, "Unable to add edge!")()

				fmt.Printf("With new edges:\n%s", g.String(show))

				// Remove those two nodes from consideration, as they are already synced
				sync = true
				break
			}

			// If a synchronization was performed, remove the two edges involved and resample
			if sync == false {
				es = es[2:]
				continue
			}
			break		
		}
	}

	ppath := func (path []int) {
		fmt.Printf("{ ")
		for i := 0; i < len(path); i++ {
			fmt.Printf("%d ", path[i])
		}
		fmt.Printf(" }\n")
	}

	// Echo all paths. This is needed since since synchronization nodes alter order
	for key, value := range map_chain_to_path {
		fmt.Printf("%d: ", key); ppath(value)
	}

	// For each path, find the edge in the given row, and adjust its number
	for chain, path := range map_chain_to_path {
		for i, j := 0, 1; i < (len(path) - 1); i, j = i+1, j+1 {
			edges := edgesAt(path[i], path[j], g)
			for _, e := range edges {
				if e.Tag == chain {
					e.Num = i
					break
				}
			}
		}
	}

	return nil
}

/*
 *******************************************************************************
 *                       Resolution: Overlapping timers                        *
 *******************************************************************************
*/

func resolve_shared_timers (ts []temporal.Temporal, us []float64, chains []int,
	g *graph.Graph) []temporal.Temporal {

    // Create all paths, and a map for associating a timer with a set of chains
	paths, map_timer_to_paths := make([][]int, len(chains)), make(map[int]([]int))

	// Create the resolved temporal data by copying the given slice
	rs := make([]temporal.Temporal, len(ts))
	copy(rs, ts)

	// Compute the paths of all chains
	for i, _ := range chains {
		paths[i] = get_path(i, chains, g)
	}

	// For each start (timer) node, collect all paths that start with it
	for chain, path := range paths {

		// Assume paths nonzero in length. Timer is first node
		timer := path[0]

		// Either initialize or add to the existing slice
		if sharing, exists := map_timer_to_paths[timer]; !exists {
			map_timer_to_paths[timer] = []int{chain}
		} else {
			map_timer_to_paths[timer] = append(sharing, chain)
		}
	}

	// For all timers, pick a single value for all chains using it
	for timer, sharing := range map_timer_to_paths {

		// Find chain that ownes timer
		timer_owner_chain := get_row_chain(timer, chains)

		// Debug
		if len(sharing) > 1 {
			fmt.Printf("More than one chain start from node %d!\n", timer)
		}

		// Update period and computation of all chains sharing the timer
		for _, chain := range sharing {
			rs[chain].T = rs[timer_owner_chain].T
			rs[chain].C = rs[chain].T * us[chain]
		}
	}

	return rs
}

/*
 *******************************************************************************
 *                        Mapping: Utilisation to nodes                        *
 *******************************************************************************
*/

type Triple struct {
	Node    int
	Chain   int
	WCET    float64
}

// Assigns WCET to all nodes within chains. Resolves clashes
func map_wcet_to_nodes (ts []temporal.Temporal, chains []int, g *graph.Graph) map[int]float64 {
	var paths [][]int = make([][]int, len(chains))
	var node_map map[int]*set.Set = make(map[int]*set.Set)
	var node_wcet_map map[int]float64 = make(map[int]float64)
	var path_redistribution_budget []float64 = make([]float64, len(chains))

	// Closure: Comparator for sets
	set_cmp := func (x, y interface{}) bool {
		a, b := x.(Triple), y.(Triple)
		return (a.Node == b.Node) && (a.Chain == b.Chain)
	}

	// Closure: Computes minimum over a set
	get_min := func (x, y interface{}) {
		min_int_p, triple := x.(*float64), y.(Triple)
		if (triple.WCET < *min_int_p) {
			*min_int_p = triple.WCET
		}
	}

	// Closure: Computes difference in WCET, places in path budget
	acc_diff := func (x, y interface{}) {
		min_value, triple := x.(float64), y.(Triple)
		fmt.Printf("Redistribution[%d] = %f - %f\n", triple.Chain, triple.WCET, min_value)
		path_redistribution_budget[triple.Chain] += (triple.WCET - min_value)
	}

	// For each chain, compute its path
	for i := 0; i < len(chains); i++ {
		paths[i] = get_path(i, chains, g)
	}

	// For each path:
	// 1. Compute the WCET for each node as (total_chain_wcet / path_length)
	// 2. Add an entry to a set with the node's WCET
	// 3. In the end, we resolve the WCET by picking the minimum
	for i, path := range paths {
		wcet := ts[i].C / float64(len(path))
		fmt.Printf("The WCET for path %d is %f\n", i, wcet)
		for _, node := range path {
			value, ok := node_map[node]
			if !ok {
				node_map[node] = &set.Set{Triple{Node: node, Chain: i, WCET: wcet}}
			} else {
				value.Insert(Triple{Node: node, Chain: i, WCET: wcet}, set_cmp)
			}
		}
	}

	// Compute WCET for each node
	for key, value := range node_map {

		// Compute the minimum value
		var min_value float64 = math.MaxFloat64
		value.MapWith(&min_value, get_min)

		// Store minimum value in node WCET
		fmt.Printf("Minimum WCET for node %d is %f\n", key, min_value)
		node_wcet_map[key] = min_value

		// Update each path with the differnece between their WCET and min
		value.MapWith(min_value, acc_diff)
	}

	// For all paths, attempt to find someplace to dump the redistribution budget
	for i, path := range paths {
		unshared_nodes := []int{}

		// Don't redistribute if there isn't anything
		if path_redistribution_budget[i] == 0.0 {
			continue
		} else {
			fmt.Printf("Path %d has a redistribution budget of %f\n", i, path_redistribution_budget[i])
		}

		// Otherwise collect unshared nodes on the path
		for _, node := range path {
			value, _ := node_map[node]
			if value.Len() == 1 {
				unshared_nodes = append(unshared_nodes, node)
			}
		}

		// If no unshared nodes, then panic (must be at least one)
		if len(unshared_nodes) == 0 {
			panic(errors.New(fmt.Sprintf("No unshared nodes in path %d", i)))
		}

		// Distribute budget across all
		fractional_budget := path_redistribution_budget[i] / float64(len(unshared_nodes))

		for _, n := range unshared_nodes {
			node_wcet_map[n] = node_wcet_map[n] + fractional_budget
		}
	}

	// Return the map
	return node_wcet_map
}


/*
 *******************************************************************************
 *                        Mapping: Benchmarks to nodes                         *
 *******************************************************************************
*/

// Maps a tuple (benchmark, repeats) to a node based on its WCET
func map_benchmarks_to_nodes (node_wcet_map map[int]float64, benchmarks []*benchmark.Benchmark) map[int]benchmark.Work {
	var node_work_map map[int]benchmark.Work = make(map[int]benchmark.Work)
	var best_fit_benchmark *benchmark.Benchmark = nil

	// Returns true if candidate divides target better than best when flooMagenta
	is_better_divisor := func (target, candidate, best float64) bool {
		diff_best := math.Abs(target - math.Floor(target / best) * best)
		diff_cand := math.Abs(target - math.Floor(target / candidate) * candidate)
		return diff_cand < diff_best 
	}

	// Locate benchmark which best divides the given WCET
	for node, wcet := range node_wcet_map {

		// Reset the best fit
		best_fit_benchmark = nil

		// Locate the best fit
		for _, benchmark := range benchmarks {

			// Ignore unset entries
			if benchmark.Runtime == 0.0 {
				continue
			}

			// Ignore candidate benchmarks whose time is > than WCET
			if benchmark.Runtime > wcet {
				continue
			}

			// Automatically select a benchmark if unset
			if best_fit_benchmark == nil {
				best_fit_benchmark = benchmark
				continue
			}

			// Otherwise compare the difference in remainder, and pick the better one
			if is_better_divisor(wcet, benchmark.Runtime, best_fit_benchmark.Runtime) {
				best_fit_benchmark = benchmark
			}
		}

		// If none found, then warn
		if best_fit_benchmark == nil {
			warn("No benchmark can represent WCET %f for node %d!", wcet, node)
			node_work_map[node] = benchmark.Work{Benchmark: nil, Iterations: 0}
			continue
		}

		// Enter into map
		iterations := int(math.Floor(wcet / best_fit_benchmark.Runtime))
		node_work_map[node] = benchmark.Work{Benchmark: best_fit_benchmark, Iterations: iterations}
	}

	return node_work_map
}

/*
 *******************************************************************************
 *                         Mapping: Priority to nodes                          *
 *******************************************************************************
*/

// Maps chains to priorities
func synthesize_node_priorities (chains, priorities []int, g *graph.Graph) map[int]int {
	node_prio_map := make(map[int]int)
	paths         := make([][]int, len(chains))

	// Closure: Simple max
	max := func (a, b int) int {
		if a > b {
			return a
		} else {
			return b
		}
	}

	// Closure: Returns the number of nodes belonging to chains
	get_chain_node_count := func (chains []int) int {
		sum := 0
		for i := 0; i < len(chains); i++ {
			sum += chains[i]
		}
		return sum
	}

	// Obtain the number of nodes that belong to chains
	n_chain_nodes := get_chain_node_count(chains)

	// Setup the paths
	for i := 0; i < len(chains); i++ {
		paths[i] = get_path(i, chains, g)
	}

	// Go down the paths of all chains. Assign all nodes MAX(old_prio, current_prio)
	for chain, path := range paths {
		priority := priorities[chain]
		for _, node := range path {
			existing_priority, exists := node_prio_map[node]
			if !exists {
				node_prio_map[node] = priority
				continue
			}
			if priority > existing_priority {
				node_prio_map[node] = priority
			}
		}
	}

	// Go down the paths of all chains. If you hit a sync, then adopt the sync value
	// and propagate all the way back down
	for i := 0; i < len(chains); i++ {
		path := paths[i]
		min_priority := priorities[i]

		for j := (len(path) - 1); j >= 0; j-- {
			node := path[j]

			// Update minimum priority if a sync node
			if node >= n_chain_nodes {
				min_priority = max(min_priority, node_prio_map[node])
			}

			// Apply minimum priority
			node_prio_map[node] = max(min_priority, node_prio_map[node])
		}
	}

	return node_prio_map
}


/*
 *******************************************************************************
 *                                    Main                                     *
 *******************************************************************************
*/

func get_benchmarks (cfg benchmark.Configuration) ([]*benchmark.Benchmark, error) {
	var benchmarks   []*benchmark.Benchmark
	var unevaluated []*benchmark.Benchmark
	var err         error
	var os          string = runtime.GOOS

	// Init the benchmark environment
	check(benchmark.Init_Env(cfg), "Initialize benchmark environment")()

	// Init the benchmarks themselves
	benchmarks, err = benchmark.Init_Benchmarks(cfg)
	check(err, "Initialize benchmarks")()

	// Locate any unevaluated benchmarks
	unevaluated, err = benchmark.Get_Unevaluated_Benchmarks(cfg, benchmarks)
	check(err, "Locate unevaluated benchmarks")()

	// Check if any need to be evaluated
	if len(unevaluated) > 0 && (os != "linux") {
		color.Warn.Printf("Benchmark evaluation not available for %s\n" + 
			"This step will be ignoMagenta - but is needed for simulating WCET\n", os)
		return benchmarks, nil
	}

	// Evaluate any unevaluated benchmarks
	for _, b := range unevaluated {
		check(benchmark.Evaluate_Benchmark("cc", cfg, 10, b), "evaluating %s", b.Name)()
	}

	return benchmarks, nil
}

// Returns a list of chains, of varying length
func get_chains (chain_count, chain_avg_len int, chain_variance float64) []int {
	var chains []int = make([]int, chain_count)

	// Standard deviation is variance squaMagenta
	std_dev := math.Sqrt(chain_variance)

	// Mean is simply our average chain length
	mean := float64(chain_avg_len)

	// Sample a normal distribution to obtain a random variable
	for i := 0; i < chain_count; i++ {
		r := rand.NormFloat64() * std_dev + mean
		chains[i] = int(math.Max(1.0, math.Round(r)))
		fmt.Printf("%f\n", r)
	}

	return chains
}

// Returns a list of random RGB color hex strings
func get_colors (n int) []string {
	chain_colors, err := colors.RandomColors(n)
	check(err, fmt.Sprintf("Unable to generate %d random colors!", n))()
	return chain_colors
}

// Returns a list of random merge operations to perform
func get_random_merges (chains []int, merge_p float64) ([]int, []int) {
	from, to := []int{}, []int{}

	// Compute number of nodes
	node_count := 0
	for _, c := range chains {
		node_count += c
	}

	// Create node array
	node_array := []int{}
	for i := 0; i < node_count; i++ {
		node_array = append(node_array, i)
	}

	// Continue to merge until only one node left
	for len(node_array) > 1 {

		// Number of possible merges between nodes
		chances := (len(node_array) * (len(node_array) - 1)) / 2

		// Take a chance for each possible merge
		for chances > 0 {

			// If probability met - merge and reconsider
			if rand.Float64() < merge_p {
				i, j := rand.Intn(len(node_array)), rand.Intn(len(node_array))

				// Don't count self merges
				if i == j {
					continue
				}

				// Otherwise register a merge
				from = append(from, node_array[i])
				to   = append(to,   node_array[j])
				node_array = append(node_array[:i], node_array[i+1:]...)
				break
			}
			chances--
		}

		// If all chances exhausted, stop
		if chances == 0 {
			return from, to
		}
	}

	return from, to
}

func main () {
	var benchmarks []*benchmark.Benchmark
	var err error
	var input Config
	var data []byte

	// Expect input as a JSON argument
	if len(os.Args) != 2 {
		reason := fmt.Sprintf("%s expects exactly 1 argument, not %d", os.Args[0], len(os.Args) - 1)
		check(errors.New(reason), "Please provide input as JSON encoded argument")()
	} else {
		data = []byte(os.Args[1])
	}

	// Attempt to decode argument
	err = json.Unmarshal(data, &input)
	check(err, "Unable to unmarshal the input argument!")()
	show_config(input)

	// Extract path
	path := input.Path
	fmt.Printf("path = %s\n", input.Path)

	// Get the benchmarks
	bench_cfg := benchmark.Configuration{Src: path + "/benchmarks/bench/sequential", Stats: "stats", Bin: "bin"}
	benchmarks, err = get_benchmarks(bench_cfg)

	// Print benchmarks
	for _, b := range benchmarks {
		fmt.Printf("%16s\t\t\t%.2f ns\t\t\t%.2f%%\t\t\t", b.Name, b.Runtime, b.Uncertainty)
		if b.Evaluated {
			color.Black.Printf("Ready\n")
		} else {
			color.Magenta.Printf("No data\n")
		}
	}

	// Generate the chains
	// TODO: Accept a graph from input
	chains := get_chains(input.Chain_count, input.Chain_avg_len, input.Chain_variance)
	colors := get_colors(input.Chain_count)
	for i, c := range chains {
		fmt.Printf("%d. %d\n", i, c)
	} 

	ppath := func (path []int) {
		fmt.Printf("{ ")
		for i := 0; i < len(path); i++ {
			fmt.Printf("%d ", path[i])
		}
		fmt.Printf(" }\n")
	}

	// Create the graph
	g := init_graph(chains, colors)
	fmt.Printf("%s", g.String(show))

	// Print paths
	for i := 0; i < input.Chain_count; i++ {
		ppath(get_path(i, chains, g))
	}

	// Perform random merges
	from, to := get_random_merges(chains, input.Chain_merge_p)
	for i := 0; i < len(from); i++ {
		fmt.Printf("%d -> %d\n", from[i], to[i])
	}

	for i := 0; i < len(from); i++ {
		if can_merge(from[i], to[i], chains, g) {
			merge(from[i], to[i], g)
		}
	}

	// Assign timing information to graph
	us := temporal.Uunifast(1.0, len(chains))
	for i := 0; i < len(us); i++ {
		fmt.Printf("Chain %d gets utilization %f\n", i, us[i])
	}
	ts := temporal.Make_Temporal_Data(temporal.Range{Min: 1000, Max: 10000}, us)
	for i := 0; i < len(ts); i++ {
		fmt.Printf("Chain %d gets (T = %f, C = %f)\n", i, ts[i].T, ts[i].C)
	}

	// TODO: Some chains may begin with the same timer, meaning their period is
	// not independent and thus their computation time needs to be fixed to match
	// utilization!
	// 1. Determine which chains are sharing timers
	// 2. Pick one
	// 3. Update the temporal data
	ts = resolve_shared_timers(ts, us, chains, g)
	fmt.Printf("Printing chains again after resolving conflicts...\n")
	periods := []float64{}
	for i := 0; i < len(ts); i++ {
		fmt.Printf("Chain %d gets (T = %f, C = %f)\n", i, ts[i].T, ts[i].C)
		periods = append(periods, ts[i].T)
	}

	// Map computation time to nodes
	node_wcet_map := map_wcet_to_nodes(ts, chains, g)
	for key, value := range node_wcet_map {
		fmt.Printf("Node %d WCET = %f\n", key, value)
	}

	// Assign benchmark to each node
	node_work_map := map_benchmarks_to_nodes(node_wcet_map, benchmarks)
	for node, work_assign := range node_work_map {
		if work_assign.Benchmark == nil {
			fmt.Printf("Node %d has been assigned nothing, since no benchmark suits it...\n", node)
		} else {
			fmt.Printf("Node %d has been assigned {.Benchmark = %s, .Iterations = %d} for %f <= %d WCET\n", 
				node, work_assign.Benchmark.Name, work_assign.Iterations, float64(work_assign.Iterations) * work_assign.Benchmark.Runtime, node_wcet_map[node])			
			}
	}

	// Extend the graph with synchronizations (which implicity lengthens the chain)
	g_sync := add_synchronization_nodes(input.Chain_sync_p, chains, g)
	fmt.Println(g_sync)

	// Print paths
	paths := []([]int){}
	fmt.Println("UPDATED PATHS AFTER SYNC")
	for i := 0; i < input.Chain_count; i++ {
		paths = append(paths, get_path(i, chains, g))
		ppath(get_path(i, chains, g))
	}

	// Synthesize prioritites for the graph
	priorities := make([]int, input.Chain_count)
	for i := 0; i < input.Chain_count; i++ {
		priorities[i] = i
		fmt.Printf("Chain %d has priority %d\n", i, i)
	}
	node_prio_map := synthesize_node_priorities(chains, priorities, g)
	for key, value := range node_prio_map {
		fmt.Printf("Node %d has prio %d\n", key, value)
	}

	// Create visualization
	gvz, err := graph_to_graphviz(chains, node_wcet_map, node_prio_map, g)
	if nil != err {
		panic(err)
	}
	err = generate_from_template(gvz, path + "/templates/graph.dt", "graph.dot")
	if nil != err {
		panic(err)
	}

	// Lookup if DOT is available (exit quietly if not)
	_, err = exec.LookPath("dot")
	if nil != err {
		return
	}

	// Otherwise use dot to produce a graph
	dot_cmd := exec.Command("dot", "-Tpng",  "graph.dot",  "-o",  path + "/graph.png")
	err = dot_cmd.Run()
	check(err, "Unable to invoke dot!")()

	// Export into some intermediate format (JSON)?
	app := gen.Init_Application("Foo", true, 3)
	app.From_Graph(chains, paths, periods, node_wcet_map, node_work_map, node_prio_map, g)

	// Create a vizualization
	apz, err := application_to_graphviz(app, g)
	check(err, "Unable to create application graphviz!")()
	err = generate_from_template(apz, path + "/templates/application.dt", "application.dot")
	check(err, "Unable to create dot file for application!")()


	// Otherwise use dot to produce a graph
	dot_cmd = exec.Command("dot", "-Tpng",  "application.dot",  "-o",  path + "/application.png")
	err = dot_cmd.Run()
	check(err, "Unable to invoke dot!")()

	// Application needs
	// - Number of executors
	// - Number of nodes (automatic)
	// - Name
	// - Using PPE
	executors := []ROS_Executor{}
	for _, exec := range app.Executors {
		executors = append(executors, ROS_Executor{
			Includes: []string{"std_msgs/msg/int64.hpp"},
			MsgType:  "std_msgs::msg::Int64",
			FilterPolicy: "message_filters::sync_policies::ApproximateTime",
			PPE:      app.PPE,
			Executor: exec,
		})
	}
	for i, exec := range executors {
		exec_name := fmt.Sprintf("executor_%d.cpp", i) 
		err = generate_from_template(exec, path + "/templates/executor.tmpl", exec_name)
		check(err, "Unable to create template file!")()
	}

	// Each method needs:
	// - is a timer
	// - period if a timer
	// - benchmark + repetitions
	// - chain major id
	// - chain minor id
	// - indicator if it is the end of the chain
	// - map:
	//     - on_sub_topic -> publish_to_topic (optionally nil)
	// - priority
	// - WCET
	// - 

	fmt.Println("Finished and graph generated!")
}