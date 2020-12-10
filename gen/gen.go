package gen

import (

	// Standard packages
	"fmt"
	"os"
	"os/exec"
	"io"
	"bufio"
	"io/ioutil"
	"text/template"
	"errors"
	"strings"

	// Custom packages
	"rosgraph/app"
)

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
	Executor     app.Executor     // The executor to parse
}

type Metadata struct {
	Packages     []string          // Packages to include in makefile
	Includes     []string          // Include directives for C++ program
	MsgType      string            // Program message type
	PPE          bool              // Whether to use PPE types and semantics
	FilterPolicy string            // Policy for message filters
	Libraries    []string          // Path to static libraries to link/copy in
	Headers      []string          // Paths to headers files to copy in
	Sources      []string          // Paths to source files to copy in
}

type Build struct {
	Name         string            // Name given to XML package
	Packages     []string          // Packages to include
	Sources      []string          // Source files to compile with executables
	Libraries    []string          // Libraries to link with executables
	Executors    []ROS_Executor    // ROS executable structures
}

/*
 *******************************************************************************
 *                         Public Function Definitions                         *
 *******************************************************************************
*/

// Generates a buffer from a template at 'path', which is fed to the given command as stdin
func GenerateWithCommand (path, command string, args []string, data interface{}) error {
	var err error = nil
	var template_buffer []byte = []byte{}
	var t *template.Template = nil

	// Check: command exists
	_, err = exec.LookPath(command)
	if nil != err {
		return errors.New("Cannot find command \"" + command + "\": " + err.Error())
	}

	// Check: valid data
	if nil == data {
		return errors.New("bad input: null pointer")
	}

	// Read in the template file
	template_buffer, err = ioutil.ReadFile(path)
	if nil != err || template_buffer == nil {
		return errors.New("Unable to read template \"" + path + "\": " + err.Error())
	}

	// Convert file to template
	t, err = template.New("Unnamed").Parse(string(template_buffer))
	if nil != err {
		return errors.New("Template parse error: " + err.Error())
	}

	// Build command to run (configure it to read from a pipe)
	cmd := exec.Command(command, args...)
	r, w := io.Pipe()
	cmd.Stdin = r

	// Run the command in a goroutine
	go func() {
		cmd.Run()
		r.Close()
	}()

	// Execute template into buffered writer
	err = t.Execute(w, data)
	defer w.Close()
	if nil != err {
		return errors.New("Exception executing template: " + err.Error())
	}

	return nil
}

// Generates a file given a data structure, path to template, and output filename
func GenerateTemplate (data interface{}, in_path, out_path string) error {
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

func GenerateApplication (a *app.Application, path string, meta Metadata) error {
	var err error = nil

	// Closure: Attempts to make all given directories
	make_directories := func (directories []string) error {
		for _, dir := range directories {
			err := os.Mkdir(dir, 0777)
			if nil != err {
				return errors.New("Cannot make dir (" + dir + "): " + err.Error())
			}
		}
		return nil
	}

	// Check: input
	if nil == a {
		return errors.New("bad argument: null pointer")
	}

	// Strip possible forward-slash from path
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = path[:len(path)-1]
	}

	// Prepare directories
	root_dir := path + "/" + a.Name
	src_dir, include_dir_1 := root_dir + "/src", root_dir + "/include"
	include_dir_2 := include_dir_1 + "/" + a.Name
	lib_dir, launch_dir := root_dir + "/lib", root_dir + "/launch"

	// Create directories
	ds := []string{root_dir, src_dir, include_dir_1, include_dir_2, lib_dir, launch_dir}
	err = make_directories(ds)
	if nil != err {
		return err
	}

	// Generate source files
	executors := []ROS_Executor{}
	for i, exec := range a.Executors {
		ros_exec_name := fmt.Sprintf("executor_%d.cpp", i)
		ros_exec := ROS_Executor{
			Includes:     meta.Includes,
			MsgType:      meta.MsgType,
			FilterPolicy: meta.FilterPolicy,
			PPE:          meta.PPE,
			Executor:     exec,
		}
		executors = append(executors, ros_exec)
		err = GenerateTemplate(ros_exec, path + "/templates/executor.tmpl", 
			src_dir + "/" + ros_exec_name)
		if nil != err {
			return errors.New("Unable to generate source file: " + err.Error())
		}
	}

	// Update the metadata
	sources, err := filenames_from_paths(meta.Sources)
	if nil != err {
		return err
	}
	libraries, err := filenames_from_paths(meta.Libraries)
	if nil != err {
		return err
	}
	build := Build{
		Name:      a.Name,
		Packages:  meta.Packages,
		Sources:   sources,
		Libraries: libraries,
		Executors: executors,
	}

	// Generate makefile
	err = GenerateTemplate(build, "templates/CMakeLists.tmpl", root_dir + "/CMakeLists.txt")
	if nil != err {
		return errors.New("Unable to generate CMakeLists: " + err.Error())
	}

	// Generate package descriptor file
	err = GenerateTemplate(build, "templates/package.tmpl", root_dir + "/package.xml")

	// Copy in libraries, headers, and source files
	err = copy_files_to(meta.Libraries, lib_dir)
	if nil != err {
		return err
	}
	err = copy_files_to(meta.Headers, include_dir_2)
	if nil != err {
		return err
	}
	err = copy_files_to(meta.Sources, src_dir)

	// Generate the launch file
	err = GenerateTemplate(build, "templates/launch.tmpl", launch_dir + "/" + build.Name + "_launch.py")

	return err
}


/*
 *******************************************************************************
 *                        Private Function Definitions                         *
 *******************************************************************************
*/

// Copies a file 
func copy_file (from, to string) error {
	file_from, err := os.Open(from)
	if nil != err {
		return err
	}
	defer file_from.Close()

	file_to, err := os.OpenFile(to, os.O_RDWR | os.O_CREATE, 0777)
	if nil != err {
		return err
	}
	defer file_to.Close()

	_, err = io.Copy(file_to, file_from)
	if nil != err {
		return err
	}
	return nil
}

// Copy files (full path) to a destination folder
func copy_files_to (paths []string, destination string) error {

	// Check if destination exists
	if !exists_file_or_directory(destination) {
		return errors.New("Unable to locate: " + destination)
	}

	// Move all files to the given directory
	for _, path := range paths {

		// Check if path exists
		if !exists_file_or_directory(path) {
			return errors.New("Unable to locate: " + path)
		}

		// Strip down to the filename
		filename, err := filename_from_path(path)
		if nil != err {
			return err
		}

		// Copy over
		err = copy_file(path, destination + "/" + filename)
		if nil != err {
			return err
		}
	}
	return nil
}

func exists_file_or_directory (path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func filename_from_path (path string) (string, error) {
	path_elements := strings.Split(path, "/")
	if len(path_elements) == 0 {
		return "", errors.New("Both separator and path are empty!")
	}
	return path_elements[len(path_elements) - 1], nil
}

func filenames_from_paths (paths []string) ([]string, error) {
	filenames := []string{}
	for _, path := range paths {
		filename, err := filename_from_path(path)
		if nil != err {
			return []string{}, err
		}
		filenames = append(filenames, filename)
	}
	return filenames, nil
}