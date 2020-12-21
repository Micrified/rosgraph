package main

import (

	// Standard packages
	"os"
	"fmt"
	"errors"
	"math"
	"math/rand"
	"runtime"
	"encoding/json"
	"io/ioutil"

	// Custom packages
	"temporal"
	"graph"
	"set"
	"benchmark"
	"colors"
	"analysis"
	"ops"
	"app"
	"gen"

	// Third party packages
	"github.com/gookit/color"
)

/*
 *******************************************************************************
 *                        Input/Output Type Definitions                        *
 *******************************************************************************
*/

type Config struct {
	Path               string
	Chain_count        int
	Chain_avg_len      int
	Chain_merge_p      float64
	Chain_sync_p       float64
	Chain_variance     float64
	Util_total         float64
	Min_period_us      int
	Max_period_us      int
	Period_step        float64
	Hyperperiod_count  int
	PPE                bool
	Executor_count     int
	Random_seed        int
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

func info (s string, args ...interface{}) {
	color.Style{color.FgGreen, color.OpBold}.Printf("%s\n", fmt.Sprintf(s, args...))
}

func show_config (cfg Config) {
	color.Blue.Printf("Path:           "); color.Green.Printf("%s\n", cfg.Path)
	color.Blue.Printf("Chain_count:    "); color.Green.Printf("%d\n", cfg.Chain_count)
	color.Blue.Printf("Chain_avg_len:  "); color.Green.Printf("%d\n", cfg.Chain_avg_len)
	color.Blue.Printf("Chain_merge_p:  "); color.Green.Printf("%.2f\n", cfg.Chain_merge_p)
	color.Blue.Printf("Chain_sync_p:   "); color.Green.Printf("%.2f\n", cfg.Chain_sync_p)
	color.Blue.Printf("Chain_variance: "); color.Green.Printf("%.2f\n", cfg.Chain_variance)
	color.Blue.Printf("Util_total:     "); color.Green.Printf("%.2f\n", cfg.Util_total)
	color.Blue.Printf("Min_period_us:  "); color.Green.Printf("%d\n", cfg.Min_period_us)
	color.Blue.Printf("Max_period_us:  "); color.Green.Printf("%d\n", cfg.Max_period_us)
}

/*
 *******************************************************************************
 *                          Graph Operation Functions                          *
 *******************************************************************************
*/

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

	// Compute all current paths for the chains
	for i := 0; i < len(chains); i++ {
		map_chain_to_path[i] = ops.PathForChain(i, chains, g)
		fmt.Printf("%s\n", ops.Path2String(map_chain_to_path[i]))
	}

	// Condition: Starting elements are special and cannot merge
	// Solution:  If 'from' and 'to' are not the same type of element - return false
	start_rows := ops.StartingRows(chains)
	if contains(from, start_rows) != contains(to, start_rows) {
		fmt.Printf("Cannot merge %d and %d, as nodes are of incompatible types!\n", from, to)
		return false
	}

	// Condition: The length of a path must not be shortened during a merge
	// Solution: If 'to' and 'from' have any direct edges between one another - return false
	if ops.EdgeBetween(from, to, g) {
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
	to_chain_id, from_chain_id := ops.ChainForRow(to, chains), ops.ChainForRow(from, chains)
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
	if ops.Disconnected(from, g) || ops.Disconnected(to, g) {
		fmt.Printf("Cannot merge %d and %d, as one of them is disconnected (already merged elsewhere)\n", from, to)
		return false
	}

	fmt.Printf("Merge (%d,%d) allowed!\n", from, to)
	return true
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
	Edge   *ops.Edge
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
		map_chain_to_path[i] = ops.PathForChain(i, chains, g)
	}

	// Locate all nodes with two or more incoming edges
	for i := 0; i < g.Len(); i++ {

		// Check column for incoming edges
		for j := 0; j < g.Len(); j++ {

			// Extract all incoming edges along column i
			edges := ops.EdgesAt(j, i, g)

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
				fmt.Printf("%s", g.String(ops.Show))

				// Expand the graph with a new node
				n := ops.ExtendGraphByOne(g)

				fmt.Printf("After:\n%s", g.String(ops.Show))

				// Update the path with the new node
				insert_in_path(a.From, (n-1), a.Edge.Tag)
				insert_in_path(b.From, (n-1), b.Edge.Tag)

				// Move edges to new node
				err = ops.RewireTo(a.From, a.To, a.Edge.Tag, a.Edge.Num, (n-1), g)
				check(err, "Unable to rewire edge")()
				err = ops.RewireTo(b.From, b.To, b.Edge.Tag, b.Edge.Num, (n-1), g)
				check(err, "Unable to rewire edge")()
				fmt.Printf("Set destination ...\n%s", g.String(ops.Show))

				// Wire node to destination
				err = ops.Wire((n-1), a.To, a.Edge.Tag, a.Edge.Num + 1, a.Edge.Color, g)
				check(err, "Unable to add edge!")()
				err = ops.Wire((n-1), b.To, b.Edge.Tag, b.Edge.Num + 1, b.Edge.Color, g)
				check(err, "Unable to add edge!")()

				fmt.Printf("With new edges:\n%s", g.String(ops.Show))

				// Remove those two nodes from consideration, as they are already synced
				sync = true
				break
			}

			// If a synchronization was performed, remove both edges involved and resample
			if sync == false {
				es = es[2:]
				continue
			}
			break		
		}
	}

	// Echo all paths. This is needed since since synchronization nodes alter order
	for key, value := range map_chain_to_path {
		fmt.Printf("%d: %s", key, ops.Path2String(value))
	}

	// For each path, find the edge in the given row, and adjust its number
	for chain, path := range map_chain_to_path {
		for i, j := 0, 1; i < (len(path) - 1); i, j = i+1, j+1 {
			edges := ops.EdgesAt(path[i], path[j], g)
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
		paths[i] = ops.PathForChain(i, chains, g)
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
		timer_owner_chain := ops.ChainForRow(timer, chains)

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
		paths[i] = ops.PathForChain(i, chains, g)
	}

	// For each path:
	// 1. Compute the WCET for each node as (total_chain_wcet / path_length)
	// 2. Add an entry to a set with the node's WCET
	// 3. In the end, we resolve the WCET by picking the minimum
	for i, path := range paths {
		wcet := ts[i].C / float64(len(path))
		fmt.Printf("The WCET for each callback along path %d is (%f / %d) = %f\n", i, ts[i].C, len(path), wcet)
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
func map_benchmarks_to_nodes (node_wcet_map map[int]float64, 
	benchmarks []*benchmark.Benchmark) map[int]benchmark.Work {
	var node_work_map map[int]benchmark.Work = make(map[int]benchmark.Work)
	var best_fit_benchmark *benchmark.Benchmark = nil

	// Returns true if candidate divides target better than best when foor'ed
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
			if benchmark.Runtime_us == 0.0 {
				continue
			}

			// Ignore candidate benchmarks whose time is > than WCET
			if benchmark.Runtime_us > wcet {
				continue
			}

			// Automatically select a benchmark if unset
			if best_fit_benchmark == nil {
				best_fit_benchmark = benchmark
				continue
			}

			// Otherwise compare the difference in remainder, and pick the better one
			if is_better_divisor(wcet, benchmark.Runtime_us, best_fit_benchmark.Runtime_us) {
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
		iterations := int(math.Floor(wcet / best_fit_benchmark.Runtime_us))
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

	// Obtain the number of nodes that belong to chains
	n_chain_nodes := ops.NodeCount(chains)

	// Setup the paths
	for i := 0; i < len(chains); i++ {
		paths[i] = ops.PathForChain(i, chains, g)
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
		path, min_priority := paths[i], priorities[i]

		for j := (len(path) - 1); j >= 0; j-- {
			node := path[j]

			// Update minimum priority if a sync node
			if node >= n_chain_nodes {
				min_priority = ops.Max(min_priority, node_prio_map[node])
			}

			// Apply minimum priority
			node_prio_map[node] = ops.Max(min_priority, node_prio_map[node])
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
			"This step will be ignored - but is needed for simulating WCET\n", os)
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

	// Standard deviation is variance squared
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

	// Expect input as a JSON file
	if len(os.Args) != 2 {
		fmt.Printf("%s <config.json>\n", os.Args[0])
		return
	}

	// Attempt to open file
	if data, err = ioutil.ReadFile(os.Args[1]); nil != err {
		check(err, fmt.Sprintf("Unable to open %s", os.Args[1]))()
		return
	}

	// Attempt to decode argument
	err = json.Unmarshal(data, &input)
	check(err, "Unable to unmarshal the input argument!")()
	show_config(input)

	// Apply random seed
	fmt.Printf("Seed = %d\n", input.Random_seed)
	rand.Seed(int64(input.Random_seed))

	// Extract path
	path := input.Path
	fmt.Printf("path = %s\n", input.Path)

	// Get the benchmarks
	bench_cfg := benchmark.Configuration{Src: path + "/benchmarks/bench/sequential", Stats: "stats", Bin: "bin"}
	benchmarks, err = get_benchmarks(bench_cfg)

	// Print benchmarks
	for _, b := range benchmarks {
		fmt.Printf("%16s\t\t\t%.2f us\t\t\t%.2f%%\t\t\t", b.Name, b.Runtime_us, b.Uncertainty)
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

	// Create the graph
	g := ops.InitGraph(chains, colors)

	// DEBUG: Print paths
	fmt.Printf("%s", g.String(ops.Show))
	for i := 0; i < input.Chain_count; i++ {
		fmt.Printf("%s\n", ops.Path2String(ops.PathForChain(i, chains, g)))
	}

	// Perform random merges
	from, to := get_random_merges(chains, input.Chain_merge_p)
	for i := 0; i < len(from); i++ {
		if can_merge(from[i], to[i], chains, g) {
			info("Approved: %d -> %d\n", from[i], to[i])
			ops.Merge(from[i], to[i], g)
		} else {
			warn("Rejected: %d -> %d\n", from[i], to[i])
		}
	}

	// Assign timing information to graph
	us := temporal.Uunifast(1.0, len(chains))
	ts, err := temporal.Make_Temporal_Data(temporal.Range{Min: float64(input.Min_period_us), 
		Max: float64(input.Max_period_us)},input.Period_step, us)
	info("Chains have a period in range [%d,%d]us, with a step of %fus", input.Min_period_us,
		input.Max_period_us, input.Period_step)
	check(err, "Unable to generate timing data")()
	for i := 0; i < len(ts); i++ {
		fmt.Printf("Chain %d gets (U = %f, T = %f, C = %f)\n", i, us[i], ts[i].T, ts[i].C)
	}
	hyperperiod, err := temporal.Integral_Hyperperiod(ts)
	check(err, "Unable to determine hyperperiod")()
	info("Hyperperiod: %dus", hyperperiod)

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
			fmt.Printf("Node %d has been assigned {.Benchmark = %s, .Iterations = %d} for %f <= %f WCET\n", 
				node, work_assign.Benchmark.Name, work_assign.Iterations, 
				float64(work_assign.Iterations) * work_assign.Benchmark.Runtime_us, node_wcet_map[node])			
			}
	}

	// Extend the graph with synchronizations (which implicity lengthens the chain)
	g_sync := add_synchronization_nodes(input.Chain_sync_p, chains, g)
	fmt.Println(g_sync)

	// Print paths
	paths := []([]int){}
	fmt.Println("UPDATED PATHS AFTER SYNC")
	for i := 0; i < input.Chain_count; i++ {
		paths = append(paths, ops.PathForChain(i, chains, g))
		fmt.Printf("%s\n", ops.Path2String(paths[i]))
	}

	// Synthesize prioritites for the graph
	// TODO: Make this something that can be changed via input
	// Right now, only chain zero is prioritized
	priorities := make([]int, input.Chain_count)
	priorities[0] = 1;
	for i := 1; i < input.Chain_count; i++ {
		priorities[i] = 0
		fmt.Printf("Chain %d has priority %d\n", i, i)
	}
	node_prio_map := synthesize_node_priorities(chains, priorities, g)
	for key, value := range node_prio_map {
		fmt.Printf("Node %d has prio %d\n", key, value)
	}

	// Convert the graph into a ROS-like representation
	app := app.Init_Application("beta", input.PPE, input.Executor_count)
	app.From_Graph(chains, paths, periods, node_wcet_map, node_work_map, node_prio_map, g)

	// Metadata for application generation
	meta := gen.Metadata {
		Packages: []string{"std_msgs", "message_filters"},
		Includes: []string{"std_msgs/msg/int64.hpp", "message_filters/subscriber.h", "message_filters/sync_policies/approximate_time.h",
							app.Name + "/" + "tacle_benchmarks.h", app.Name + "/" + "roslog.h"},
		MsgType:        "std_msgs::msg::Int64",
		PPE:            app.PPE,
		FilterPolicy:   "message_filters::sync_policies::ApproximateTime",
		Libraries:      []string{path + "/lib/libtacle.a"},
		Headers:        []string{path + "/lib/tacle_benchmarks.h", path + "/include/roslog.h"},
		Sources:        []string{path + "/src/roslog.cpp"},
		Duration_us:    int64(input.Hyperperiod_count) * hyperperiod,
	}

	// Graphdata for application generation
	graph_data := gen.Graphdata {
		Chains:        chains,
		Node_wcet_map: node_wcet_map,
		Node_prio_map: node_prio_map,
		Graph:         g,
	}

	// Generate the application
	err = gen.GenerateApplication(app, path, meta, graph_data)
	check(err, "Unable to generate application")()
	info("Nominal")

	// Warn that utilisation does not apply if synchronization enabled
	if input.Chain_sync_p > 0.0 {
		warn("Nonzero chance of sync nodes, means that utilisation does not apply as expected!")
	}

	// Write the chains file, which is used for analysis
	periods_int := []int{}
	for _, p := range periods {
		periods_int = append(periods_int, int(p))
	}
	err = analysis.WriteChains(path + "/chains.json", chains, periods_int, priorities, paths, us)
	check(err, "Unable to generate chains analysis file")()

	// Todo:
	// 1. Make sure two versions of each application are generated (vanilla, ppe)
	// 2. Make sure this is working for sync
	// 3. Generate a launch file too
	// 4. Finalize some kind of logging

	// Todo: 
	// Convert timing to microseconds due to overflows
	// when computing hyperperiod for large values

	// Assign priorities using rate-monotonic
	// (this means your sufficient test is via the hyperbolic bound, which says)
	// The sum of all utilizations, each with 1 added, is less than or equal to 2 (see internet)
	// The rate-monotonnic priority assignment is optimal, meaning that if any static priority
	// scheduling algorithm can meet all deadlines, then rate monotonic can too. 
	// Simple rate-monotonic analysis assumes
	// 1. No resource sharing
	// 2. deadlines = periods
	// 3. static priorities (preemption)
	// 4. static priorities are assigned according to rate monotonic convetntions
	// 5. no impact of context switch times
	//
	// Right now, all I do is make sure system passes necessary condition. But it still
	// might not be schedulable. 

	// TODO: Place the generates image files WITH the code in a separate folder
	// Also include the chains file

	// TODO: Consider assignment to cores
	// E.G: Given a number of cores, distribute them to run on each core independently.
	// Maybe not as good as letting them run as is, but controllable

	// TODO:
	// You can compare the effect of chain length without interfering effects
	// of shared callbacks or sync nodes first. Those will be testing your synthesis 
	// policy

	// TODO:
	// Sort logs in chronological order, so that you don't screw up the log interpreter
}