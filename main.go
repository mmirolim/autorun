// fast hack to auto run Go source after changes
package main

import (
	"context"
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
	appArgs         = flag.String("args", "", "arguments to pass to binary format '-k1 v1 -k2v2'")
	excludePrefixes = flag.String("exclude", ".,#,flymake,#flymake", "prefixes to exclude sep by comma")

	excludeDirs = flag.String("exclude-dirs", "vendor,node_modules", "exclude directories from watching")

	delayDuration           time.Duration
	excludeFilePrefixesList []string
	excludeDirsList         []string
)

func init() {
	flag.Parse()
}

func main() {
	fmt.Println("autorun running...")
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		fmt.Printf("%+v\n", err) // output for debug
		os.Exit(1)
	}
	defer watcher.Close()

	excludeFilePrefixesList = strings.Split(*excludePrefixes, ",")
	excludeDirsList = strings.Split(*excludeDirs, ",")

	delayDuration = time.Duration(*delay) * time.Millisecond

	watchDir(watcher, ".")
	// start listening
	startWatching(watcher, strings.Split(*appArgs, " "))
}

// main loop to listen all events from all registered directories
// and exec required commands, kill previously started process, build new and start it
func startWatching(watcher *fsnotify.Watcher, args []string) {
	var (
		lastModFile string
		lastModTime time.Time
		ctx         context.Context
		cancel      context.CancelFunc
	)

LOOP:
	for {
		select {
		case e := <-watcher.Events:
			if skipChange(e, lastModFile, lastModTime) {
				continue LOOP
			}
			log.Println("File changed:", e.Name)

			lastModFile = e.Name
			lastModTime = time.Now()
			if cancel != nil {
				cancel()
			}
			ctx, cancel = context.WithCancel(context.Background())
			// do not block loop
			go runCmds(ctx, args)

		case err := <-watcher.Errors:
			log.Println("Error:", err)
		}
	}
}

// cmd sequence to run build with some name, check err and run named binary
func runCmds(ctx context.Context, args []string) error {
	// build binary
	cmd := exec.CommandContext(ctx, "go", "build", "-o", "goapp")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	err := cmd.Run()
	if err != nil {
		fmt.Printf("go build error %+v\n", err) // output for debug
		return err
	}

	// run binary
	cmd = exec.CommandContext(ctx, "./goapp", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	err = cmd.Start()
	if err != nil {
		fmt.Printf("./goapp error %+v\n", err) // output for debug
		return err
	}

	return cmd.Wait()
}

// recursively set watcher to all child directories
// and fan-in all events and errors to chan in main loop
func watchDir(watcher *fsnotify.Watcher, dir string) error {
	// walk directory and if there is other directory add watcher to it
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if !info.IsDir() {
			return err
		}
		if skipDir(path) {
			return filepath.SkipDir
		}
		err = watcher.Add(path)
		if err != nil {
			return err
		}
		return nil
	})
	return err
}

func skipChange(e fsnotify.Event, lastModFile string, lastModTime time.Time) bool {
	if e.Op&(fsnotify.Write|fsnotify.Create) == 0 {
		return true
	}
	if lastModFile == e.Name {
		if time.Since(lastModTime) <= delayDuration {
			return true
		}
	}
	if !strings.HasSuffix(e.Name, ".go") {
		return true
	}
	name := path.Base(e.Name)
	for _, prefix := range excludeFilePrefixesList {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func skipDir(path string) bool {
	if path == "." {
		return false
	}
	baseDir := filepath.Base(path)
	// skip hidden dirs
	if strings.HasPrefix(baseDir, ".") {
		return true
	}
	for _, name := range excludeDirsList {
		if baseDir == name {
			return true
		}
	}
	return false
}
