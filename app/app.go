package app

// Author: Charles Randolph
// Function:
//    Gen (short for "generate") provides:
//    1. A structure hierarchy more appropriate for representing a ROS app
//    2. A structure hierarchy that can be easily used with templates
//    3. Facilities for converting:
//
//        a. An edge graph 
//        b. A list of chain lengths, and their respective paths
//        c. A list of periods for the chains
//        d. Maps linking graph nodes to WCET, benchmarks, and priorities
//       
//      Into this described hierarchy. It also automatically decides how many
//      nodes belong in each executor, and randomly assigns callbacks across
//      executors. 
//
//    Info: Callback placement
//      A callback is always placed in a node that belongs to its chain. This
//      decision is meant to replicate the concept that chains of callbacks
//      typically need to share data. So it makes sense that they be grouped
//      together.
//
//    Info: Filter placement
//      Filters, or sync nodes, are always placed inside the node that they
//      forward to. This decision is meant to mimic the most likely placement
//      of such nodes in a real ROS application. Namely, that given you want
//      to synchronize inputs to a certain callback, that the filter would be
//      setup in the same node as that of the callback.


import (

	// Standard packages
	"fmt"
	"math/rand"
	"errors"
	// "encoding/json"
	// "bytes"
	// "os"

	// Custom packages
	"graph"
	"benchmark"
)

/*
 *******************************************************************************
 *                              Type Definitions                               *
 *******************************************************************************
*/

type Callback struct {
	ID          int          // Unique identifier
	Priority    int          // Priority for use with PPE
	Timer       bool         // True if a timer callback
	Period      int64        // Timer period (set if timer) in us
	WCET        int64        // WCET (us) simulated
	Benchmark   string       // Name of benchmark for WCET
	Repeats     int          // Times to repeat benchmark
	Topics_rx   []int        // [1] Topic map rx[i] -> tx[i]
	Topics_tx   []int        // [2] Topic map rx[i] -> tx[i]
	Topics_cx   []int        // [3] Chain identifier for [1,2]
}

type Filter struct {
	ID          int          // Unique identifier
	Topics_rx   []int        // [1] Topic map rx[i] -> tx[i]
	Topics_tx   []int        // [2] Topic map rx[i] -> tx[i]
	Topics_cx   []int        // [3] Chain identifier for [1,2]
}

type Node struct {
	ID          int          // Unique node identifier
	Callbacks   []Callback   // Callbacks located in node
	Filters     []Filter     // Message filters located in node
}

type Executor struct {
	ID          int          // Unique executor identifier
	Nodes       []Node       // Nodes located in executor
}

type Application struct {
	Name        string       // Application identifer
	PPE         bool         // Assumes PPE semantics if set true
	Executors   []Executor   // Executors composing application
}


/*
 *******************************************************************************
 *                         Public Function Definitions                         *
 *******************************************************************************
*/


func Init_Application (name string, ppe bool, exec_count int) *Application {
	var app Application = Application{
		Name: name, 
		PPE: ppe, 
		Executors: make([]Executor, exec_count),
	}

	// Init empty executors
	for i := 0; i < exec_count; i++ {
		app.Executors[i].ID = i
		app.Executors[i].Nodes = []Node{}
	}

	return &app
}

func (a *Application) From_Graph (
	chains        []int,                  // List of chain lengths
	paths         [][]int,                // Paths of the chains
	periods       []float64,              // Period of the chains
	node_wcet_map map[int]float64,        // Maps a node to a WCET
	node_work_map map[int]benchmark.Work, // Maps a node to a benchmark
	node_prio_map map[int]int,            // Maps a node to a priority
	g             *graph.Graph) error {   // 2D graph with node relations
	var err error = nil

	// Maps nodes to either a callback or filter
	node_callback_map, node_filter_map := make(map[int]Callback), make(map[int]Filter)

	// Determine the number of callbacks and filters
	n_callbacks := get_callback_count(chains)

	// Chain represents the id of the path, and path is the slice of visited nodes
	for chain, path := range paths {
		for i := 0; i < len(path); i++ {
			var from, to, node int

			// Setup current node, previous, and next
			node = path[i]
			if i == 0 {
				from = -1
			} else {
				from = path[i-1]
			}
			if i == (len(path) - 1) {
				to = -1
			} else {
				to = path[i+1]
			}

			// It's a callback if node < n_callbacks, otherwise it's a filter
			if node < n_callbacks {

				// If an entry does not exist - create one;
				entry, exists := node_callback_map[node]
				if !exists {
					benchmark_name := ""
					if node_work_map[node].Benchmark != nil {
						benchmark_name = node_work_map[node].Benchmark.Name
					}
					entry = Callback{
						ID:        node,
						Priority:  node_prio_map[node],
						Timer:     (i == 0),
						Period:    int64(periods[get_row_chain(node, chains)]),
						WCET:      int64(node_wcet_map[node]),
						Benchmark: benchmark_name,
						Repeats:   node_work_map[node].Iterations,
						Topics_rx: []int{},
						Topics_tx: []int{},
						Topics_cx: []int{},
					}
				}

				// Enter in the relation
				entry.Topics_rx = append(entry.Topics_rx, from)
				entry.Topics_tx = append(entry.Topics_tx, to)
				entry.Topics_cx = append(entry.Topics_cx, chain)
				node_callback_map[node] = entry
			} else {

				// If an entry does not exist - create one;
				entry, exists := node_filter_map[node]
				if !exists {
					entry = Filter {
						ID:        node,
						Topics_rx: []int{},
						Topics_tx: []int{},
						Topics_cx: []int{},
					}
				}

				// Enter in the relation
				entry.Topics_rx = append(entry.Topics_rx, from)
				entry.Topics_tx = append(entry.Topics_tx, to)
				entry.Topics_cx = append(entry.Topics_cx, chain)
				node_filter_map[node] = entry
			}
		}
	}

	// Extract callbacks and filters as the values of the respective maps
	callbacks, filters := []Callback{}, []Filter{}
	for _, value := range node_callback_map {
		callbacks = append(callbacks, value)
	}
	for _, value := range node_filter_map {
		filters = append(filters, value)
	}

	// Distribute callbacks into executors
	err = a.distribute_callbacks(chains, callbacks)
	if nil != err {
		return err
	}
	err = a.distribute_filters(filters)
	if nil != err {
		return err
	}

	// // Debug
	// data, _ := json.Marshal(*a)
	// fmt.Printf("\n\n")
 //    fmt.Println(string(data))

 //    var out bytes.Buffer
	// json.Indent(&out, data, "=", "\t")
	// out.WriteTo(os.Stdout)


	return nil
}


/*
 *******************************************************************************
 *                        Private Function Definitions                         *
 *******************************************************************************
*/


// Distributes callbacks into an application (Strategy: Group by chain)
func (a *Application) distribute_callbacks (chains []int, callbacks []Callback) error {
	executor_callback_pool := make([][]Callback, len(a.Executors))

	// Verify that there are at least as many callbacks as executors
	// Why? Because we group filters with callbacks
	if len(callbacks) < len(a.Executors) {
		reason := fmt.Sprintf("More executors than callbacks (%d callbacks, %d executors)",
			len(callbacks), len(a.Executors))
		return errors.New(reason)
	}

	// Randomly shuffle the callbacks
	rand.Shuffle(len(callbacks), func(i, j int){ callbacks[i], callbacks[j] = callbacks[j], callbacks[i] })

	// Allocate callbacks to the executors
	for i := 0; i < len(callbacks); i++ {
		e := i % len(a.Executors)
		executor_callback_pool[e] = append(executor_callback_pool[e], callbacks[i])
	}

	// Determine the number of nodes needed per executor. The number of different
	// chains in the callbacks is used to determine this
	for i, callback_pool := range executor_callback_pool {
		node_chain_map := make(map[int]Node)

		// For each callback alloted to this executor
		for _, callback := range callback_pool {

			// Get the chain ID for the callback
			chain_id := get_row_chain(callback.ID, chains)

			// Check if there is a node present for the chain of the callback
			node, exists := node_chain_map[chain_id]

			// If not, then create one 
			if !exists {
				node = Node{ID: len(node_chain_map), Callbacks: []Callback{}, Filters: []Filter{}}
			}

			// Add the callback to the node
			node.Callbacks = append(node.Callbacks, callback)

			// Save it
			node_chain_map[chain_id] = node
		}

		// Collect all nodes
		nodes, j := make([]Node, len(node_chain_map)), 0
		for _, node := range node_chain_map {
			nodes[j] = node
			j++
		}

		// Update the executor
		a.Executors[i].Nodes = nodes
	}

	return nil
}

// Distributes filters into an application (Strategy: Group with dest callback)
// Assumes: Nodes created, and callbacks already distributed
func (a *Application) distribute_filters (filters []Filter) error {

	// Closure: Return a pair (index, index) of an executor and node containing the given callback
	get_indices := func (callback_id int) (int, int) {
		for i, e := range a.Executors {
			for j, n := range e.Nodes {
				for _, c := range n.Callbacks {
					if c.ID == callback_id {
						return i, j
					}
				}
			}
		}
		return -1, -1
	}

	for _, f := range filters {

		// Check that the destination node exists for the filter
		if len(f.Topics_tx) == 0 {
			reason := fmt.Sprintf("Filter %d has no destination node!", f.ID)
			return errors.New(reason)
		}

		// Locate the index of the executor, and inner node, that hold that destination
		id_e, id_n := get_indices(f.Topics_tx[0])

		// Return an error it none was found, otherwise insert it
		if id_e == -1 {
			reason := fmt.Sprintf("Cannot place filter %d, destination callback not found!",
				f.ID)
			return errors.New(reason)
		} else {
			a.Executors[id_e].Nodes[id_n].Filters = append(a.Executors[id_e].Nodes[id_n].Filters, f)
		}
	}

	return nil
}

// Given a slice of chain lengths, it returns the number of callbacks
func get_callback_count (chains []int) int {
	n := 0
	for _, c := range chains {
		n += c
	}
	return n
}

// Returns the chain to which the given row belongs to 
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
