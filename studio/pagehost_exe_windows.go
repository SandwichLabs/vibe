//go:build windows

package main

type exeRecord struct{}

func recordExe() exeRecord { return exeRecord{} }
