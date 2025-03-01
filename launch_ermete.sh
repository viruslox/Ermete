#!/bin/bash
#### this is just an example, You might add XDG runtime folder and Xauthority path

## Choose the discord command prefix, copy here Your discord bot token
export GOLIVE_BOT_PREFIX="ermete."
export GOLIVE_BOT_TOKEN="<discord token"
export ENABLE_AUDIO_OUTPUT="NO" # yes or NO

# Run the Go program
echo "Starting the Go bot..."

# Set here the name of Your Ermete compiled executable
executable="Ermete"
# Set here the path of Your Ermete compiled executable
executable_path="/opt/Ermete/${executable}"

## This prevent Ermete to be launched again if it's already running
pids=$(pidof "$executable")
if [[ -z "$pids" ]]; then
	"$executable_path"
else
    echo "'$executable' is already running."
fi

exit 0
