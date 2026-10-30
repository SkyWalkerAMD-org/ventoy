package main

import (
	"errors"
	"os"
)

func setNice(int, int) error             { return errors.New("Linux only") }
func rootOnly() error                    { return errors.New("Linux only; Windows is supported for fixture tests only") }
func secureDirectory(string) error       { return errors.New("Linux only") }
func lockState(string) (*os.File, error) { return nil, errors.New("Linux only") }
func selfcheck() error                   { return errors.New("Linux only") }
