package sample

import "os"

func helper() {
	os.Exit(1) // should trigger Lint/OsExit
}

func doNothing() { // should trigger Style/EmptyFunc
}

func process() (int, error) {
	return 0, nil
}

func caller() {
	_, e := process() // should trigger Style/ErrorNaming
	_ = e
}
