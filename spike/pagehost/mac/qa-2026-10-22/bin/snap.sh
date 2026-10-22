#!/bin/bash
# snap.sh <file>: the helper and every WebKit service, with ppid and elapsed time
ps -axo pid,ppid,etime,command | grep -E 'vibe-studio|com.apple.WebKit' | grep -v grep | sed 's#/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/##' > "$1"
