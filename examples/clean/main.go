package main

import "os"

func main() {
	os.Exit(0) // should NOT trigger — it's in main()
}
