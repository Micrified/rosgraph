package main 

import (
	"os"
	"fmt"
	"errors"
	"text/template"
	"io/ioutil"
	"set"
	"bufio"
	"math/rand"
)

/*
 *******************************************************************************
 *                          Types for Callback Chains                          *
 *******************************************************************************
*/

type Timer struct {
	Period_ns           int                      // Nanosecond period of timer
}

type Callback struct {
	Name                string
	Chain_id            int                      // ID of owner chain (global)
	Chain_index         int                      // Index in chain (global)
	Timer               *Timer                   // Set if a source node
}

type Topic struct {
	Name                string                   // Topic name (auto set)
}

type Chain struct {
	Callbacks           []*Callback              // Callbacks in call order
}

type Graph struct {
	Chains              []*Chain                 // All chains
	Topics              []*Topic                 // Slice holding topics
	Callback_Topics     map[*Callback]*set.Set   // Maps Callback to topic set
	Topic_Callbacks     map[*Topic]*set.Set      // Maps Topic to Callback set
}

type Merge struct {
	From                *Callback                // Callback being replaced
	To                  *Callback                // Callback receiving merge
}

/*
 *******************************************************************************
 *                              Types for Digraph                              *
 *******************************************************************************
*/

type Edge struct {
	From                 string                   // ID of source node (global)
	To                   string                   // ID of dest node (global)
	Color                string                   // Chain color (#RRGGBB)
}

type Node struct {
	Name                 string                   // ID of node (global)
	Callbacks            []*Callback              // Callbacks within node
}

type Executor struct {
	Name                 string                   // ID of executor (global)
	Nodes                []*Node                  // Nodes within executor
}

type Digraph struct {
	Callbacks            []*Callback              // Slice of all callback
	Topics               []*Topic                 // Slice of all topics
	Executors            []*Executor              // Slice of all executors
	Edges                []*Edge                  // Slice of all edges
}

type Config struct {
	N_Executors          int                       // Number of executors to use
}

/*
 *******************************************************************************
 *                         Chain Generation Functions                          *
 *******************************************************************************
*/


func init_graph () *Graph {
	g := Graph{
		Chains:             []*Chain{},
		Topics:             []*Topic{},
		Callback_Topics:    make(map[*Callback]*set.Set),
		Topic_Callbacks:    make(map[*Topic]*set.Set),
	}
	return &g
}

func init_chain (id, length, period int, graph *Graph) {
	var cs []*Callback
	var ts []*Topic

	// Inline function which creates entity name
	name := func(prefix rune, index int) string {
		return fmt.Sprintf("%c_%d_%d", prefix, id, index)
	}

	// Create callbacks
	for i := 0; i < length; i++ {
		c := &Callback{
			Name:        name('C', i),
			Chain_id:    id,
			Chain_index: i,
			Timer:       nil,
		}
		cs = append(cs, c)
	}

	// Make first callback a timer
	if length > 0 {
		cs[0].Timer = &Timer{Period_ns: period}
	}

	// Create topics 
	for i := 0; i < (length - 1); i++ {
		t := &Topic{Name: name('T', i)}
		ts = append(ts, t)
	}

	// Map callbacks to topics, and vice versa
	for i := 0; i < (length - 1); i++ {
		graph.Callback_Topics[cs[i]] = &set.Set{ts[i]}
		graph.Topic_Callbacks[ts[i]] = &set.Set{cs[i+1]}
	}

	// Last callback publishes nowhere
	if length > 0 {
		graph.Callback_Topics[cs[(length - 1)]] = &set.Set{}
	}

	// Update graph with chain and topic data (maps already updated)
	graph.Chains = append(graph.Chains, &Chain{Callbacks: cs})
	graph.Topics = append(graph.Topics, ts...)
}

func cmp (a, b interface{}) bool {
	x := a.(*Callback)
	y := b.(*Callback)
	return (x.Chain_id == y.Chain_id) && (x.Chain_index == y.Chain_index)
}

func cmp_topic (a, b interface{}) bool {
	x := a.(*Topic)
	y := b.(*Topic)
	return (x.Name == y.Name)
}

func show (a interface{}) {
	x := a.(*Callback)
	fmt.Printf("(%d.%d)", x.Chain_id, x.Chain_index)
}


/*
 *******************************************************************************
 *                        Chain Manipulation Functions                         *
 *******************************************************************************
*/

// Basic rules to follow when manipulating callback chains
//
// 1. Source nodes (timers) may only be merged with each other. This is because
//    merging them with ordinary callbacks is an irrational design choice in 
//    real applications, and it also changes the length of the chain. We want
//    to make sure the chain lengths stay within parameters
//
// 2. A chain must have at least one independent callback. If they are all 
//    merged, then it ceases to be an independent chain. Thus, either the
//    source must remain independent, or at least one callback must remain
//    un-merged from that chain
//
// 3. Callbacks which are merged should spend the same amount of time executing, 
//    or else the function of the callback does not really make sense. For every
//    shared callback, it must assume a utilization value that works for all 
//    chains that happen to share it
//
// 4. Synchronized nodes are problematic because the waiting interferes with the
//    utilization (how do you know what your utilization will be). It's a form of
//    self suspension and can rely on many things. I have no current solution to
//    this problem
//
// 5. You may not merge a callback from chain A with another callback which 
//    already was merged with a previous callback from chain A

func merge_random_callbacks (p float64, g *Graph) error {
	merge_queue := []Merge{}

	// Place all callbacks in an array
	cs := []*Callback{}
	for _, chain := range g.Chains {
		for _, callback := range (*chain).Callbacks {
			cs = append(cs, callback)
		}
	}

	// Closure: Returns callback as string
	cb2s := func(a *Callback) string {
		return fmt.Sprintf("%d.%d", a.Chain_id, a.Chain_index)
	}

	// Closure: Returns true if both callbacks are the right type
	compatibility_match := func(a, b *Callback) bool {
		return !((a.Timer == nil) != (b.Timer == nil))
	}

	// Closure: Returns true if the following conditions hold:
	//          1. 'from' has not been merged with a prior chain member
	//              from 'to'
	//          2. 'to' has not been merged with a prior chain member
	//              from 'from'
	//          3. They are not from the same chain
	merge_match := func (to, from *Callback) bool {

		// 3.
		if (to.Chain_id == from.Chain_id) {
			return false
		}

		for _, m := range merge_queue {

			// 1.
			if (cmp(from, m.From) && m.To.Chain_id == to.Chain_id) ||
			   (cmp(from, m.To)   && m.From.Chain_id == to.Chain_id) {
			   	return false
			}

			// 2.
			if (cmp(to, m.To) && m.To.Chain_id == from.Chain_id) ||
			   (cmp(to, m.From) && m.From.Chain_id == from.Chain_id) {
			   	return false
			}
		}

		return true
	}

	// Make the order random
	rand.Shuffle(len(cs), func(i, j int) { cs[i], cs[j] = cs[j], cs[i] })

	// Iterate down the array, comparing the ith to those in range [i+1, N) 
	// and merging if:
	// 1. Probability value is obtained
	// 2. Not in violation of other rules
	for i := 0; i < (len(cs) - 1); i++ {
		for j := i+1; j < len(cs); j++ {
			a, b := cs[i], cs[j]

			// Chance (if fails, don't merge)
			if rand.Float64() >= p {
				continue
			}

			// Chance + compatibility check
			if !compatibility_match(a, b) {
				fmt.Printf("Incompatible: %s v %s\n", cb2s(a), cb2s(b))
				continue
			}

			// Chance + compatible + merge check
			if !merge_match(a,b) {
				fmt.Printf("Previous merge clash: %s v %s\n", cb2s(a), cb2s(b))
				continue
			}

			// Merge approved
			merge_queue = append(merge_queue, Merge{From: b, To: a})
		}
	}

	// Apply the merges
	// 1. Find which topics publish to 'from'; foreach: replace 'from' with 'to'
	// 2. Find which topics 'from' publishes; foreach: insert into 'to' publish set
	// 3. Replace 'from' in its chain with 'to'
	for _, m := range merge_queue {
		fmt.Printf("Merging %s into %s\n", cb2s(m.From), cb2s(m.To))
		// Find all topics that publish to callback: 'from'
		for _, topic := range g.Topics {
			out_set := g.Topic_Callbacks[topic]

			// If that topic publishes to 'from': Now remove and set to 'to'
			if out_set.Contains(m.From, cmp) {
				out_set.Remove(m.From, cmp)
				out_set.Insert(m.To, cmp)
			}
		}

		// Add all topics that are published to from callback: 'from'
		// to callback 'to'
		out_set := g.Callback_Topics[m.From]
		add_set := g.Callback_Topics[m.To]
		add_set.Union(out_set, cmp_topic)

		// Replace Callback 'from' with Callback 'to' in its chain
		g.Chains[m.From.Chain_id].Callbacks[m.From.Chain_index] = m.To
	}

	return nil
}

/*
 *******************************************************************************
 *                        Digraph Generation Functions                         *
 *******************************************************************************
*/

func make_digraph (cfg Config, g *Graph) Digraph {
	callbacks := []*Callback{}
	topics    := []*Topic{}
	edges     := []*Edge{}
	executors := []*Executor{}
	colors    := []string{}

	// Closure: Random color
	random_color := func () string {
		r := fmt.Sprintf("%x", rand.Uint64() % 256)
		g := fmt.Sprintf("%x", rand.Uint64() % 256)
		b := fmt.Sprintf("%x", rand.Uint64() % 256)
		return ("#" + r + g + b)
	}

	// Closure: Add topic to slice
	append_topic := func (z interface{}, x interface{}) {
		slice := z.(*[]*Topic)
		topic := x.(*Topic)
		(*slice) = append((*slice), topic)
	}

	// Closure: Add callback to slice
	append_callback := func (z interface{}, x interface{}) {
		slice := z.(*[]*Callback)
		callback := x.(*Callback)
		(*slice) = append((*slice), callback)
	}

	// Closure: Add edges from callback to all topics
	insert_callback_edges := func (from *Callback, to_all []*Topic) {
		for _, to := range to_all {
			color := colors[from.Chain_id]
			edges = append(edges, &Edge{From: from.Name, To: to.Name, Color: color})
		}
	}

	// Closure: Add edges from topic to all callbacks
	insert_topic_edges := func (from *Topic, to_all []*Callback) {
		for _, to := range to_all {
			color := colors[to.Chain_id]
			edges = append(edges, &Edge{From: from.Name, To: to.Name, Color: color})
		}
	}

	// Closure: Returns random permutation of executors
	random_executor_permutation := func () []*Executor {
		x := []*Executor{}
		for _, e := range executors {
			x = append(x, e)
		}
		rand.Shuffle(len(x), func(i, j int) { x[i], x[j] = x[j], x[i] })
		return x
	}

	// Create a color for every chain
	for i := 0; i < len(g.Chains); i++ {
		colors = append(colors, random_color())
	}

	// Create all executors, with a node for every chain
	for i, j := 0, 0; i < cfg.N_Executors; i++ {
		exec_name := fmt.Sprintf("Executor_%d", i)
		nodes := []*Node{}
		for k := 0; k < len(g.Chains); k, j = k + 1, j + 1 {
			node_name := fmt.Sprintf("Node_%d", j)
			nodes = append(nodes, &Node{Name: node_name, Callbacks: []*Callback{}})
		}
		executors = append(executors, &Executor{Name: exec_name, Nodes: nodes})
	}

	// Distribute all callbacks
	// 1. Try to spread callbacks across all executors if possible.
	//    The sequence of the executors chosen should be random
	// 2. Place the callback in a node associated with its chain
	//    within the executor. This follows the notion of shared 
	//    data between components of a chain. Not all nodes may
	//    be used, depending on whether some have been merged
	// 3. Because some callbacks may have been merged, there is
	//    a risk of duplicate insertions into more than one node
	//    callback slice. To avoid this, keep a set of what nodes
	//    have been inserted and track it
	visited := set.Set{}
	for _, chain := range g.Chains {
		sequence := random_executor_permutation()
		for i, callback := range (*chain).Callbacks {

			// Ignore visited callback; else insert it
			if visited.Contains(callback, cmp) {
				continue;
			} else {
				visited.Insert(callback, cmp)
			}

			// Obtain next executor from random permutation
			executor := sequence[i % len(sequence)]

			// Obtain the node holding callbacks for chain the callback
			// belongs to
			node := (*executor).Nodes[(*callback).Chain_id]

			// Insert the callback into that node
			node.Callbacks = append(node.Callbacks, callback)
		}
	}

	// Create all the edges in the graph
	for _, chain := range g.Chains {
		for _, callback := range (*chain).Callbacks {
			callbacks = append(callbacks, callback)
			out_set := g.Callback_Topics[callback]
			out := []*Topic{}			
			out_set.MapWith(&out, append_topic)
			insert_callback_edges(callback, out)
		}
	}
	for _, topic := range g.Topics {
		topics = append(topics, topic)
		out_set := g.Topic_Callbacks[topic]
		out := []*Callback{}
		out_set.MapWith(&out, append_callback)
		insert_topic_edges(topic, out)
	}

	return Digraph{Callbacks: callbacks, Topics: topics, Executors: executors, Edges: edges}
}

/*
 *******************************************************************************
 *                        Template Generation Functions                        *
 *******************************************************************************
*/


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

func main () {

	// Seed random generator
	// rand.Seed(86)

	// Init a graph and some chains
	g := init_graph()

	// Init some chains
	init_chain(0, 3, 1000, g)
	init_chain(1, 1, 1000, g)
	init_chain(2, 10, 100, g)

	// Introduce some random merges
	merge_random_callbacks(0.15, g)

	d := make_digraph(Config{N_Executors: 3}, g)
	fmt.Printf("End of d: %d\n", len(d.Edges))

	err := generate_from_template(d, "templates/graph.dt", "chains.in")
	if err != nil {
		panic(err)
	}
	err = generate_from_template(d, "templates/application.dt", "application.in")
	if err != nil {
		panic(err)
	}

	// show_chain(c)
}