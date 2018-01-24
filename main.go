// fast hack to auto run Go source after changes
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

var (
	delay           = flag.Int("delay", 1000, "delay in Milliseconds")
	fileExt         = flag.String("ext", "go", "file extension to watch")
	showDebug       = flag.Bool("debug", false, "show debug information")
	appName         = flag.String("name", "goapp", "name builded binary")
	appArgs         = flag.String("args", "", "arguments to pass to binary format -k1=v1 -k2=v2")
	excludePrefixes = flag.String("exclude", "flymake,#flymake", "prefixes to exclude sep by comma")
)

func init() {
	flag.Parse()
}

func main() {
	fmt.Println("autorun running...")
	watchEvents := make(chan fsnotify.Event)
	watchErrors := make(chan error)
	watchDir(watchEvents, watchErrors, ".")
	// set command and args
	// default application name set as goapp
	args := strings.Split("go build -o "+*appName, " ")
	done := make(chan bool)

	// start listening to notifications in separate goroutine
	go startWatching(watchEvents, watchErrors, ".", args, *appArgs)

	// block
	<-done
}

// main loop to listen all events from all registered directories
// and exec required commands, kill previously started process, build new and start it
func startWatching(wEv chan fsnotify.Event, wE chan error, dir string, args []string, appArgs string) {
	stop := make(chan bool)
	// run required commands for the first time
	err := runCmds(*appName, args, appArgs, stop)
	if err != nil {
		log.Fatal(err)
	}
	// keep track of reruns
	rerunCounter := 1
LOOP:
	for {
		select {
		case e := <-wEv:
			for _, excludePrefix := range strings.Split(*excludePrefixes, ",") {
				name := path.Base(e.Name)
				// filter all files except .go not test files
				if e.Op&fsnotify.Write != fsnotify.Write || !strings.HasSuffix(name, "."+*fileExt) || strings.HasPrefix(name, excludePrefix) || strings.HasSuffix(name, "_test.go") {
					continue LOOP
				}
			}

			log.Println("File changed:", e.Name)
			// send signal to stop previous command
			stop <- true
			//@TODO check for better solution without sleep, had some issues with flymake emacs go plugin
			time.Sleep(time.Duration(*delay) * time.Millisecond)
			// run required commands
			err := runCmds(*appName, args, appArgs, stop)
			if err != nil {
				log.Fatal(err)
			}
			// process started incr rerun counter
			rerunCounter++

			// add loging
			debug("command executed")

		case err := <-wE:
			log.Println("Error:", err)
		}
	}
}

// cmd sequence to run build with some name, check err and run named binary
func runCmds(app string, bin []string, appArgs string, stop chan bool) error {
	// execute command
	debug("arguments for CMD", bin)
	err := newCmd(bin[0], bin[1:]).Run()
	if err != nil {
		return err
	}
	// run binary
	// do not wait process to finish
	// in case of console blocking programs
	// split binary commands by space
	cmd := newCmd("./"+app, strings.Split(appArgs, " "))
	err = cmd.Start()
	if err != nil {
		return err
	}

	go func() {
		<-stop
		// kill process if already running
		// try to kill process
		debug("process to kill pid", cmd.Process.Pid)
		err := cmd.Process.Kill()
		if err != nil {
			fmt.Println("cmd process kill returned error" + err.Error())
		}
		err = cmd.Wait()
		if err != nil {
			fmt.Println("cmd process wait returned error" + err.Error())
		}
	}()

	return nil
}

// create new cmd in standard way
func newCmd(bin string, args []string) *exec.Cmd {
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd
}

// recursively set watcher to all child directories
// and fan-in all events and errors to chan in main loop
func watchDir(watchEvents chan fsnotify.Event, watchErrors chan error, dir string) {
	// walk directory and if there is other directory add watcher to it
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			if len(path) > 1 && strings.HasPrefix(filepath.Base(path), ".") {
				return filepath.SkipDir
			}
			// create new watcher
			watcher, err := fsnotify.NewWatcher()
			if err != nil {
				log.Fatal(err)
			}
			// add watcher to dir
			err = watcher.Add(path)
			if err != nil {
				errClose := watcher.Close()
				log.Fatal(errClose, err)
			}
			debug("dir to watch", path)
			go func() {
				for {
					select {
					case v := <-watcher.Events:
						// on event send data to shared event chan
						watchEvents <- v
					case err := <-watcher.Errors:
						// on error send data to shared error chan
						watchErrors <- err
					}
				}
			}()
		}
		return err
	})
	if err != nil {
		fmt.Println("filepath walk err " + err.Error())
	}
}

func debug(args ...interface{}) {
	// check flag for log level
	if *showDebug {
		log.Println(args...)
	}
}
