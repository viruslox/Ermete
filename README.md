# Ermete: Discord Voice Bot with Real-Time Audio Processing

Ermete is a Discord bot designed for real-time audio processing and playback. It utilizes the PortAudio library for audio I/O and Opus for audio encoding, enabling (possibly) seamless interaction with Discord voice channels. This bot scope is live audio streaming.

✨ Features
Join and Leave Commands: Easily control the bot's presence in voice channels.
Real-Time Audio Streaming: Captures input from your microphone and streams it directly to Discord voice channels.
Opus Encoding: Efficiently encodes audio for Discord's voice API.
Custom Audio Processing: Extendable structure to add real-time audio effects or manipulations.


🚀 Getting Started
Prerequisites
Go (1.18+ recommended)
PortAudio library installed on your system
A Discord bot token

Environment variables set:
GOLIVE_BOT_TOKEN: Your bot's token
ERMETE_INPUT_DEVICE: Input device name (e.g., microphone)
ERMETE_OUTPUT_DEVICE: Output device name (e.g., speakers)
Installation
Clone the repository:
bash
Copy code
git clone https://github.com/yourusername/ermete.git
cd ermete

Install dependencies:
bash
Copy code
go mod tidy
Build the bot:

bash
Copy code
go build -o ermete
Run the bot:

bash
Copy code
./ermete
🛠 Usage
Commands
Join a Voice Channel:
Use gl.join <channelID> to make the bot join a voice channel.

Leave a Voice Channel:
Use gl.leave to make the bot disconnect.

Shutdown the Bot:
Use gl.shutdown (admin only) to stop the bot.

📚 Architecture
Core Components
Input Device: Captures audio from the user's microphone using PortAudio.
Opus Encoder: Encodes captured audio into Opus format for Discord compatibility.
Output Device: Plays audio received from the Discord voice channel back to the user.
Extendable Design
The project uses interfaces (ErmeteInput and ErmeteOutput) to abstract input/output devices. This design allows for easy integration of additional audio processing features or support for alternative audio backends.

🌟 Contributions
Contributions are welcome! Please fork the repository and submit a pull request.

📝 License
This project is licensed under the GPL 3.0 License. See the LICENSE file for details.
In a nutshell: if you make a derivative work of this, and distribute it to others under certain circumstances, then you have to provide the source code under this license.

📧 Contact
For questions or feedback, please reach out via the repository's Issues section.
