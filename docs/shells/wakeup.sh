#! /bin/sh
export LED_PARENT=wakeup.sh

logger -t wakeup.sh -p 3 "$*"

is_wifi_ready()
{
    STRING=`wpa_cli status 2>/dev/null` || exit 0
    echo $STRING | grep -q 'wpa_state=COMPLETED' || exit 0
}

play_wakeup()
{
    ubus -t 1 call mediaplayer player_wakeup {\"action\":\"start\"}
    /usr/bin/voip_helper -e wakeup_start & > /dev/null 2>&1
    ubus -t 1 call ai_crontab remove_all &> /dev/null 2>&1
}

play_wakeup_first()
{
    ubus -t 1 call mediaplayer player_wakeup {\"action\":\"start\"}
    /usr/bin/voip_helper -e wakeup_start & > /dev/null 2>&1
    ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/first_voice.opus\",\"type\":$1}
}

play_wakeup_oneshot()
{
    ubus -t 1 call mediaplayer player_wakeup {\"action\":\"start\"}
    /usr/bin/voip_helper -e wakeup_start & > /dev/null 2>&1
}

WuW_audio_upload()
{
    local vendor=$(micocfg_speech_vendor)
    #local time=$(TZ=cst-8 date +%y%m%d%H%M%S)
    local time=$(date -u +%y%m%d%H%M%S)
    #local channel=$(micocfg_channel)

    killall pns_upload_helper > /dev/null 2>&1

    #if [ "release" = $channel ] && [ "misWuW" != $2 ]; then
    #    return 0
    #fi

    if [ -d /tmp/mipns/upload/ ]; then
        rm -rf /tmp/mipns/upload/* > /dev/null 2>&1
    else
        mkdir /tmp/mipns/upload/ > /dev/null 2>&1
    fi

    if [ "nuance" = $vendor ]; then
        cp -r /tmp/mipns/nuance/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        /usr/bin/pns_upload_helper nuance $2 & > /dev/null 2>&1
        /usr/bin/pns_upload_helper nuance ASR & > /dev/null 2>&1
    elif [ "soundai" = $vendor ]; then
        cp /tmp/mipns/soundai/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        rm /tmp/mipns/soundai/wuw_dump/* > /dev/null 2>&1
        cp /tmp/mipns/xiaomi/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        rm /tmp/mipns/xiaomi/wuw_dump/* > /dev/null 2>&1
        #if [ "misWuW" = $1 ]; then
        #    /usr/bin/pns_upload_helper soundai $1 $time $2 & > /dev/null 2>&1
        #fi
        # soundai mono-channel
        /usr/bin/pns_upload_helper soundai $1 $time $2 & > /dev/null 2>&1
        # xiaomi original
        /usr/bin/pns_upload_helper xiaomi Original $time $2 & > /dev/null 2>&1
    elif [ "gmems" = $vendor ]; then
        cp /tmp/mipns/gmems/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        rm /tmp/mipns/gmems/wuw_dump/* > /dev/null 2>&1
        #cp /tmp/mipns/xiaomi/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        #rm /tmp/mipns/xiaomi/wuw_dump/* > /dev/null 2>&1
        # gmems mono-channel
        /usr/bin/pns_upload_helper gmems $1 $time $2 & > /dev/null 2>&1
        # xiaomi original
        #/usr/bin/pns_upload_helper xiaomi Original $time $2 & > /dev/null 2>&1
    elif [ "horizon" = $vendor ]; then
        cp /tmp/mipns/horizon/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        rm /tmp/mipns/horizon/wuw_dump/* > /dev/null 2>&1
        cp /tmp/mipns/xiaomi/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        rm /tmp/mipns/xiaomi/wuw_dump/* > /dev/null 2>&1
        # horizon mono-channel
        /usr/bin/pns_upload_helper horizon $1 $time $2 & > /dev/null 2>&1
        # horizon original
        /usr/bin/pns_upload_helper horizon Original $time $2 & > /dev/null 2>&1
    elif [ "xiaomi" = $vendor ]; then
        cp /tmp/mipns/xiaomi/wuw_dump/* /tmp/mipns/upload/ > /dev/null 2>&1
        rm /tmp/mipns/xiaomi/wuw_dump/* > /dev/null 2>&1
        # xiaomi mono-channel
        /usr/bin/pns_upload_helper xiaomi $1 $time $2 & > /dev/null 2>&1
        # xiaomi original
        /usr/bin/pns_upload_helper xiaomi Original $time $2 & > /dev/null 2>&1
    fi
}

bf_end_show(){
    local hardware=$(micocfg_model)

    if [ "LX01" = $hardware ] || [ "LX05A" = $hardware ] || [ "LX05" = $hardware ] || [ "L07A" = $hardware ]; then
        sleep 1
    fi
    /bin/show_led 11 "$1"
}

play_tips() {
    if [ "$1" = "" ]; then
        index=`date '+%s'`
        index=$((index%6))
        sound_file=tip_ai.opus
        if [ $index -eq  1 ]; then
            sound_file=tip_en.opus
        elif [ $index -eq  2 ]; then
            sound_file=tip_shaodengo.opus
        elif [ $index -eq  3 ]; then
            sound_file=tip_shenme.opus
        elif [ $index -eq  4 ]; then
            sound_file=tip_shuoba.opus
        elif [ $index -eq  5 ]; then
            sound_file=tip_zaine.opus
        fi
    else
        sound_file=$1
    fi
    ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/${sound_file}\",\"type\":2}
}

play_tone() {
    ubus call pnshelper event_notify {\"src\":3,\"event\":12}
    logger -t wakeup.sh -p 3 "tone tts start"
    qplayer $1
    logger -t wakeup.sh -p 3 "tone tts end"
    ubus call pnshelper event_notify {\"src\":3,\"event\":13}  
}

case "$1" in
    WuW)
	play_wakeup
    ;;
    WuW_oneshot)
	play_wakeup_oneshot
    ;;
    WuW_first)
	play_wakeup_first 7
    ;;
    WuW_first_unregister)
	play_wakeup_first 1
    ;;
    WuW_uploading)
	WuW_audio_upload $2
    ;;
    WuW_next_song)
	ubus -t 1 call mediaplayer player_play_operation {\"action\":\"next\",\"media\":\"wakeup_local\"}
    ;;
    bf)
	# angle
	hardware=$(micocfg_model)
        if [ "L17A" = $hardware ]; then
		# in continues mode, bf called serval times
		[ -n "$2" ] && nice -n-20 /bin/show_led 1 "$2"
	else
		nice -n-20 /bin/show_led 1 "$2"
	fi
	;;
    bf_end)
	# angle
    /bin/shut_led 1
	bf_end_show "$2"
	;;
    ready)
	nice -n-20 /bin/show_led 11 "$2"
	nice -n-20 /bin/shut_led 2
	ubus -t 1 call mediaplayer player_wakeup {\"action\":\"stop\"}
	/usr/bin/voip_helper -e wakeup_end & > /dev/null 2>&1
	hardware=$(micocfg_model)
	if [ "L09A" = $hardware ] || [ "L09B" = $hardware ] || [ "LX06" = $hardware ] || [ "L06A" = $hardware ]; then
		nice -n-20 /bin/shut_led 11
	fi
	if [ "L17A" = $hardware ]; then
		/bin/shut_led 11
		/bin/shut_led 1
	fi
	#WuW_audio_upload $3 $4
	;;
    ready_delay)
	sleep 2
	nice -n-20 /bin/show_led 11 "$2"
	nice -n-20 /bin/shut_led 2
	ubus -t 1 call mediaplayer player_wakeup {\"action\":\"stop\"}
	/usr/bin/voip_helper -e wakeup_end & > /dev/null 2>&1
	hardware=$(micocfg_model)
	if [ "L09A" = $hardware ] || [ "L09B" = $hardware ] || [ "LX06" = $hardware ] || [ "L06A" = $hardware ]; then
		nice -n-20 /bin/shut_led 11
	fi
	if [ "L17A" = $hardware ]; then
		/bin/shut_led 11
		/bin/shut_led 1
	fi
	#WuW_audio_upload $3 $4
	;;
    continous_md_end)
    ubus -t 1 call mediaplayer player_wakeup {\"action\":\"stop\"}
	/usr/bin/voip_helper -e wakeup_end & > /dev/null 2>&1
    ;;
    noangle)
	nice -n-20 /bin/show_led 9
	;;
    noangle_end)
	sleep 1
	/bin/shut_led 9
	;;
    stop)
	is_wifi_ready
	nice -n-20 /bin/shut_led 9
	ubus -t 1 call mediaplayer player_wakeup {\"action\":\"stop\"}
	;;
	wakeup_1)
	;;
	wakeup_2)
    ubus -t 1 call alarm wakeup_notify &> /dev/null 2>&1
	;;
	asrstart)
	nice -n-20 /bin/show_led 41
	;;
	asrend)
	nice -n-20 /bin/shut_led 41
	;;
	think)
	nice -n-20 /bin/show_led 2 "$2"
	;;
    speek)
	nice -n-20 /bin/show_led 3
	;;
    multirounds)
	ubus -t 1 call qplayer play {\"play\":\"/usr/share/common_sound/multirounds_tone.opus\"}
	ubus -t 1 call mediaplayer player_wakeup {\"action\":\"multistart\"}
	;;
    tone)
    play_tone $2
    ;;
    command_timeout)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/command_timeout.opus\",\"type\":1}
        ;;
    wifi_disconnect)
        sleep 2
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/wifi_disconnect.opus\",\"type\":1}
        ;;
    internet_disconnect)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/internet_disconnect.opus\",\"type\":1}
        ;;
    mibrain_connect_timeout)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_service_unavailable.opus\",\"type\":1}
        ;;
    mibrain_service_timeout)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_service_unavailable.opus\",\"type\":1}
        ;;
    mibrain_network_unreachable)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_service_unavailable.opus\",\"type\":1}
        ;;
    mibrain_service_unreachable)
        sleep 2
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_service_unavailable.opus\",\"type\":1}
        ;;
    mibrain_auth_failed)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_auth_failed.opus\",\"type\":1}
        ;;
    mibrain_start_failed)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_service_unavailable.opus\",\"type\":1}
        ;;
    mibrain_unregistered)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/mibrain_auth_failed_loading.opus\",\"type\":1}
        ;;
    upgrade_now)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/upgrade_now.opus\",\"type\":1}
        ;;
    upgrade_later)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/upgrade_later.opus\",\"type\":1}
        ;;
    wuw_tips)
        play_tips "$2"
        ;;
    welcome)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/common_sound/welcome.opus\",\"type\":1}
        ;;
    welcome_sync)
        show_led 3 1>/dev/null 2>/dev/null
        nice -n -10 miplayer -f /usr/share/common_sound/welcome.opus 1>/dev/null 2>/dev/null
        shut_led 3 1>/dev/null 2>/dev/null
        ;;
    unpair_adv_timeout)
        ubus -t 1 call mediaplayer player_play_url {\"url\":\"file:///usr/share/sound/wifi_timeout_exit_config.opus\",\"type\":1}
        ;;
esac
