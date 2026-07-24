package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func main() {
	_, filename, _, _ := runtime.Caller(0)
	fmt.Println("Current test filename: " + filename)
	os.Chdir(filepath.Dir(filename))
}
