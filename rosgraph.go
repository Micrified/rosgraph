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

	// If the chain has only one element - it cannot be merged so return row
	if chains[id] == 1 {
		return []int{start_rows[id]}
	}

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

	// Closure: Returns true if the given path has a cycle
	cycle_in_path := func (path []int) bool {
		visited := make(map[int]int)
		for i := 0; i < len(path); i++ {
			if _, exists := visited[path[i]]; exists {
				return true 
			} else {
				visited[path[i]] = 1
			}
		}
		return false
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
	path_to, path_from := get_path(to_chain_id, chains, g), get_path(from_chain_id, chains, g)
	if to_chain_id == from_chain_id || cycle_in_path(replace_in_path(from, to, path_from)) {
		fmt.Printf("Cannot merge %d and %d, as a cycle is formed in a path!\n", from, to)
		return false
	}

	// Condition: Chains with only one callback cannot be merged (shortcut for next rule)
	// Solution: If either node belongs to chain of length 1 - return false
	if chains[to_chain_id] == 1 || chains[from_chain_id] == 1 {
		fmt.Printf("Cannot merge %d and %d, as one of the chains will not be distinguishable\n", from, to)
	}

	// Condition: Don't allow chains to be completely merged with one another
	// Solution: If node being merged is the only differing element between the paths
	//           of the 'to' and 'from', then disallow merge
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

// Converts internal graph representation to graphviz data structure
func graph_to_graphviz (chains []int, temporal_map map[int]float64, g *graph.Graph) (Graphviz, error) {
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

	length_one_chain := func (row int, chains []int) bool {
		return chains[get_row_chain(row, chains)] == 1
	} 

	// Create all nodes (but only if connected or chain has length 1)
	for i := 0; i < g.Len(); i++ {
		if is_connected(i, g) || length_one_chain(i, chains) {
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
 *                       Adding: Synchronization points                        *
 *******************************************************************************
*/

// A minimal Edge, which holds the source and destination node
type MinEdge struct {
	From   int
	To     int
}

// Creates an extended version of the supplied graph by adding synchronization points
func add_synchronization_nodes (chain_sync_p float64, g *graph.Graph) *graph.Graph {
	node_edge_map := make(map[int]([]MinEdge))

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
			min_edges := []MinEdge{}
			for k := 0; k < len(edges); k++ {
				min_edges = append(min_edges, MinEdge{From: j, To: i})
			}

			// Otherwise append them to the existing slice
			if slice := node_edge_map[i]; slice != nil {
				node_edge_map[i] = append(slice, min_edges...)
			} else {
				node_edge_map[i] = min_edges
			}
		} 
	}

	// Perform a random merge between two of the pairs
	for node, edges := range node_edge_map {
		fmt.Printf("Node %d has %d incoming edges\n", node, len(edges))

		// Compute the number of attempts possible
		n := len(edges)
		attempts := (n * (n - 1)) / 2

		// Attempt 
		for i := 0; i < attempts; i++ {

			// Continue if inverse P 
			if (rand.Float64() >= chain_sync_p) {
				continue
			}

			// Otherwise shuffle
			rand.Shuffle(n, func(x, y int){ edges[x], edges[y] = edges[y], edges[x] })

			// Pick first two
			first, second := edges[0], edges[1]

			// Issue a directive
			fmt.Printf("Will be placing a sync node between %d->%d, and %d->%d\n",
				first.From, first.To, second.From, second.To)
		}
	}

	return nil
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

// Encodes a benchmark, and how many times it should be repeated to estimate a WCET
type Work struct { 
	Benchmark      *benchmark.Benchmark
	Iterations     int
}

// Maps a tuple (benchmark, repeats) to a node based on its WCET
func map_benchmarks_to_nodes (node_wcet_map map[int]float64, benchmarks []*benchmark.Benchmark) map[int]Work {
	var node_work_map map[int]Work = make(map[int]Work)
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
			node_work_map[node] = Work{Benchmark: nil, Iterations: 0}
			continue
		}

		// Enter into map
		iterations := int(math.Floor(wcet / best_fit_benchmark.Runtime))
		node_work_map[node] = Work{Benchmark: best_fit_benchmark, Iterations: iterations}
	}

	return node_work_map
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
	check(err, fmt.Sprintf("Unable to generate %d random colors!", n))
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
	check(err, "Unable to unmarshal the input argument!")
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
	g_sync := add_synchronization_nodes(input.Chain_sync_p, g)
	fmt.Println(g_sync)

	// Synthesize prioritites for the graph
	// vs := synthesize_priorities(chains, g)

	// Create visualization
	gvz, err := graph_to_graphviz(chains, node_wcet_map, g)
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
	check(err, "Unable to invoke dot!")

	// Export into some intermediate format (JSON)?

	// Application needs
	// - Number of executors
	// - Number of nodes (automatic)
	// - Name
	// - Using PPE

	// Each method needs:
	// - benchmark + repetitions
	// - chain major id
	// - chain minor id
	// - map:
	//     - on_sub_topic -> publish_to_topic (optionally nil)
	// - priority
	// - WCET
	// - 

	fmt.Println("Finished and graph generated!")
}