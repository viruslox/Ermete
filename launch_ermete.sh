#!/bin/bash
#### this is just an example, You might add XDG runtime folder and Xauthority path
#### I suggest call this script by crontab 

LOGFILE=/opt/Ermete/rc.ermete.log

## Choose the discord command prefix, copy here Your discord bot token
export GOLIVE_BOT_PREFIX="ermete."
export GOLIVE_BOT_TOKEN="<discord token"
export ENABLE_AUDIO_OUTPUT="NO" # yes or NO

# Run the Go program
date >> $LOGFILE
echo "Checking the Go bot..." >> $LOGFILE

# Executable file name
executable="Ermete"

# Absolute path
executable_path="/opt/Ermete/${executable}"

# Look for the running ones
pids=$(pidof "$executable")

# Apparently in certain situation opus decoding goes crazy and use all available memory, swap and even tmpfs :)
if [[ $(df -k /tmp | grep tmpfs | awk '{print $5}' | sed 's;%;;g') -gt 90 ]]; then 
	echo "/tmp full, killing the bot" >> $LOGFILE
	killall Ermete
wait
	killall ermete
wait
fi

# If not running, finally launch the bot
if [[ -z "$pids" ]]; then
	date >> $LOGFILE
	echo "Launching the bot" >> $LOGFILE
	nohup "$executable_path" > /opt/Ermete/Ermete.log 2>&1 &
else
    echo "'$executable' already running." >> $LOGFILE
fi

date >> $LOGFILE
echo "Done." >> $LOGFILE

exit 0
