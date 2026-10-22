//go:build !(linux && webkit2_41) && !darwin

package main

import "errors"

// nativeAvailable is false: this build has no native page window (Windows, or
// Linux without the webkit2_41 tag), so Studio refuses to open one and the
// helper mode refuses to start.
const nativeAvailable = false

// errNoNativeView is why a window cannot be opened by a binary built without a
// native web view.
var errNoNativeView = errors.New("this build has no page window")

func nativeRun(nativeConfig, nativeHooks) error  { return errNoNativeView }
func nativeRespond(uint64, bool, string, []byte) {}
func nativeEval(string)                          {}
func nativeQuit()                                {}
func nativeTakeInput() bool                      { return false }
