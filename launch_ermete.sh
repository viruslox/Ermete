#!/bin/bash
#### this is just an example, depending on the use case, You might need to find Your XDG runtime folder and Xauthority

## Choose the discord command prefix, copy here Your discord bot token
export GOLIVE_BOT_PREFIX="gl."
export GOLIVE_BOT_TOKEN="<discord token"

## pulseaudio settings -> these have to match your device names
export ERMETE_INPUT_DEVICE="Virtual.Ermete.sink.monitor"
export ERMETE_OUTPUT_DEVICE="Virtual.Ermete.out.playback"

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
