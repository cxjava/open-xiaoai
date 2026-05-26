#!/bin/sh

case "$1" in
    shutdown)
	#ubus call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/shutdown.mp3\",\"type\":1}
	ubus -t 1 call qplayer play {\"play\":\"/usr/share/common_sound/shutdown.opus\"}
	;;
    ble)
	ubus call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/ble.mp3\",\"type\":1}
	;;
    setup)
	ubus call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/setupdone.mp3\",\"type\":1}
esac
