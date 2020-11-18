package main

import (
	"os"
	"fmt"
	"errors"
	"text/template"
	"io/ioutil"
	"bufio"
	"math"
	"runtime"
	"encoding/json"
	"os/exec"

	// Custom packages
	"temporal"
	"graph"
	"set"
	"benchmark"

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
	From      int          // Source node
	To        int          // Destination node
	Color     string       // Link color
	Label     string       // Link label
}

type Node struct {
	Id        int          // Node ID
	Label     string       // Label for the node
	Style     string       // Border style
	Fill      string       // Color indicating fill of the node
}

type Graphviz struct {
	Nodes     []Node       // Pointer to nested clusters
	Links     []Link       // Pointer to slice of links
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

// Returns starting row of all chains, given an ordered slice of chain lengths
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

// Returns sequence of nodes (rows) visited by a path (chain)
func get_path (id int, chains []int, g *graph.Graph) []int {
	var row, col int = -1, -1
	var path []int = []int{}

	// Closure: Returns column index if given edge found along row, else -1
	col_for_edge := func (tag, num, row int, g *graph.Graph) int {
		col := -1
		for i := 0; i < g.Len(); i++ {
			edges := edgesAt(row, i, g)
			if len(edges) == 0 {
				continue
			}
			for _, e := range edges {
				if e.Tag == tag && e.Num == num {
					col = i
					break
				}
			}
		}
		return col
	}

	// Chains may only merge between start nodes. Starting edge is in this row
	start_rows := get_start_rows(chains)

	// Locate row on which starting edge resides
	for i := 0; i < len(start_rows); i++ {
		if c := col_for_edge(id, 0, start_rows[i], g); c != -1 {
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
		col = col_for_edge(id, len(path) - 1, row, g)
	}

	// Assert: Number of nodes 
	if len(path) != chains[id] {
		panic(errors.New(fmt.Sprintf("Node count inconsistent for chain %d", id)))
	}

	return path
}

/*
 *******************************************************************************
 *                               Graph Functions                               *
 *******************************************************************************
*/

// Initializes a new graph using given chains. Edges for each chain assigned a color
func init_graph (chains []int, palette []string) *graph.Graph {
	rows := 0;

	// Required rows is sum of chain lengths
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

	// Closure: Returns true if slice contains value
	contains := func (x int, xs []int) bool {
		for _, y := range xs {
			if x == y {
				return true
			}
		}
		return false
	}

	// Closure: Returns true if given paths are identical 
	equal_paths := func (a, b []int) bool {
		if len(a) != len(b) {
			return false
		}
		for i := 0; i < len(a); i++ {
			if a[i] != b[i] {
				return false
			}
		}
		return true
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

	// Condition: Starting elements are special and cannot merge with non-starting elements
	// Solution:  If 'from' and 'to' are not the same type of element - return false
	start_rows := get_start_rows(chains)
	if contains(from, start_rows) != contains(to, start_rows) {
		fmt.Printf("Cannot merge %d and %d, as they are not of the same type!\n", from, to)
		return false
	}

	// Condition: The length of a path must not be shortened during a merge
	// Solution: If 'to' and 'from' have any direct edges between one another - return false
	if edge_between(from, to, g) {
		fmt.Printf("Cannot merge %d and %d, as there is a direct edge between them!\n", from, to)
		return false
	}

	// Condition: A path cannot have loops
	// Solution: If 'to' and 'from' are from the same chain - return false
	to_chain_id, from_chain_id := get_row_chain(to, chains), get_row_chain(from, chains)
	if to_chain_id == from_chain_id {
		fmt.Printf("Cannot merge %d and %d, as they are from the same chain!\n", from, to)
		return false
	}

	// Condition: Don't allow chains to be completely merged with one another
	// Solution: If node being merged is the only differing element between the paths
	//           of the 'to' and 'from', then disallow merge
	path_to, path_from := get_path(to_chain_id, chains, g), get_path(from_chain_id, chains, g)
	if equal_paths(path_to, replace_in_path(from, to, path_from)) {
		fmt.Printf("Cannot merge %d and %d, as the chains will not be distinguishable\n", from, to)
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

	// Create buffered writer
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

// Converts internal graph representation to graphviz data structure
func graph_to_graphviz (temporal_map map[int]float64, g *graph.Graph) (Graphviz, error) {
	nodes := []Node{}
	links := []Link{}

	is_connected := func (row int, g *graph.Graph) bool {
		for i := 0; i < g.Len(); i++ {
			row_edges, col_edges := edgesAt(row, i, g), edgesAt(i, row, g) 
			if (len(row_edges) + len(col_edges)) > 0 {
				return true
			}
		}
		return false
	}

	// Create all nodes (but only if connected)
	for i := 0; i < g.Len(); i++ {
		if is_connected(i, g) {
			label := fmt.Sprintf("N%d\n(wcet=%.2f)", i, temporal_map[i])
			nodes = append(nodes, Node{Id: i, Label: label, Style: "solid", Fill: "#FFFFFF"})			
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

	return Graphviz{Nodes: nodes, Links: links}, nil
}

/*
 *******************************************************************************
 *                            Utilization Functions                            *
 *******************************************************************************
*/

type Triple struct {
	Node    int
	Chain   int
	WCET    float64
}

// Assigns WCET to all nodes within chains. Resolves clashes
func map_temporal_data_to_nodes (ts []temporal.Temporal, chains []int, g *graph.Graph) map[int]float64 {
	var paths [][]int = make([][]int, len(chains))
	var node_map map[int]*set.Set = make(map[int]*set.Set)
	var node_wcet map[int]float64 = make(map[int]float64)
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
		fmt.Printf("redis[%d] = %f - %f\n", triple.Chain, triple.WCET, min_value)
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
		node_wcet[key] = min_value

		// Update each path with the differnece between their WCET and min
		value.MapWith(min_value, acc_diff)
	}

	// For all paths, attempt to find someplace to dump the redistibution budget
	for i, path := range paths {
		unshared_nodes := []int{}

		// Don't redistibute if there isn't anything
		if path_redistribution_budget[i] == 0.0 {
			continue
		} else {
			fmt.Printf("Path %d has a redist budget of %f\n", i, path_redistribution_budget[i])
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
			node_wcet[n] = node_wcet[n] + fractional_budget
		}
	}

	// Return the map
	return node_wcet
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
			"This step will be ignored - but is needed for simulating WCET\n", os)
		return benchmarks, nil
	}

	// Evaluate any unevaluated benchmarks
	for _, b := range unevaluated {
		check(benchmark.Evaluate_Benchmark("cc", cfg, 10, b), "evaluating %s", b.Name)()
	}

	return benchmarks, nil
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
	check(err, "Unable to unmarshal the input argument!")
	fmt.Println(string(data))

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
			color.Green.Printf("Ready\n")
		} else {
			color.Red.Printf("No data\n")
		}
	}

	// Generate the chains

	chains, colors := []int{4,4}, []string{"red", "blue"}

	ppath := func (path []int) {
		fmt.Printf("{ ")
		for i := 0; i < len(path); i++ {
			fmt.Printf("%d ", path[i])
		}
		fmt.Printf(" }\n")
	}

	// Init the benchmarks

	// Create the graph
	g := init_graph(chains, colors)
	fmt.Printf("%s", g.String(show))

	// Print paths
	p1, p2 := get_path(0, chains, g), get_path(1, chains, g)
	ppath(p1) 
	ppath(p2)

	// Merge sets
	from := []int{0, 1, 7, 6, 3, 2, 6, 5, 2, 4, 5}
	to   := []int{3, 3, 3, 3, 6, 6, 1, 6, 6, 0, 1}

	for i := 0; i < len(from); i++ {
		if can_merge(from[i], to[i], chains, g) {
			merge(from[i], to[i], g)
			fmt.Printf("%s", g.String(show))
		}
	}

	// Print paths
	p1, p2 = get_path(0, chains, g), get_path(1, chains, g)
	ppath(p1) 
	ppath(p2)

	// Assign timing information to graph
	us := temporal.Uunifast(1.0, len(chains))
	for i := 0; i < len(us); i++ {
		fmt.Printf("Chain %d gets utilization %f\n", i, us[i])
	}
	ts := temporal.Make_Temporal_Data(temporal.Range{Min: 1000, Max: 10000}, us)
	for i := 0; i < len(ts); i++ {
		fmt.Printf("Chain %d gets (T = %f, C = %f)\n", i, ts[i].T, ts[i].C)
	}
	tps := map_temporal_data_to_nodes(ts, chains, g)
	for key, value := range tps {
		fmt.Printf("Node %d WCET = %f\n", key, value)
	}

	// Create graph
	gvz, err := graph_to_graphviz(tps, g)
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
	dot_cmd := exec.Command("dot", "-Tpng",  "graph.dot",  "-o", "graph.png")
	err = dot_cmd.Run()
	check(err, "Unable to invole dot!")

	fmt.Println("Finished and graph generated!")
}