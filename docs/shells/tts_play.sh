#!/bin/sh

source /usr/share/libubox/jshn.sh

# Help function to display usage information
usage() {
    echo "Usage: $0 \"text_to_speech\" [OPTIONS]"
    echo "Options:"
    echo "  -l, --loop          loop playback (default: false)"
    echo "  -i, --interval SEC  loop interval (seconds) (default: 1 second for loop, not applicable for non-loop)"
    echo "  -k, --keep-audio    keep audio file for non-loop playback (default: delete for non-loop, no delete for loop)"
    echo "  -d, --delete-audio  force delete audio file (default: disabled)"
    echo "  -n, --notify-pns    enable event notifications (default: disabled)"
    echo "  -h, --help          display this help message"
    echo ""
    echo "Default config:"
    echo "  - loop: interval 1 second, do not delete audio files (can use -d option to force delete)"
    echo "  - non-loop: delete audio files (can use -k option to keep)"
    exit 1
}

# Log function to log messages
my_log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S')[$0] - $1"
}

# Notification function to send event notifications to pns(vpm)
notify_pns_vpm() {
    local event_type="$1"
    local notify_pns="$2"

    if [ "$notify_pns" = "1" ]; then
        my_log "Sending notification event: {\"src\":3,\"event\":$event_type}"
        ubus call pnshelper event_notify '{"src":3,"event":'$event_type'}'
    else
        my_log "Notifications disabled, skipping"
    fi
}

# Playback function to play the audio
play_text() {
    local text="$1"
    local delete_audio="$2"
    local loop_play="$3"

    local _path=""

    # If loop playback and audio file exists, use the existing file
    if [ "$loop_play" = "1" ] && [ -n "$CACHED_AUDIO_PATH" ] && [ -f "$CACHED_AUDIO_PATH" ]; then
        _path="$CACHED_AUDIO_PATH"
        my_log "Using cached audio file for loop playback: $_path"
    else
        # Create a new audio file
        local _result=$(ubus call mibrain text_to_speech "{\"text\":\"$text\",\"save\":1}")
        [ -z "$_result" ] && {
            my_log "Error: Failed to call text_to_speech service"
            exit 1
        }

        json_init
        json_load "$_result"
        json_get_var _info info
        json_cleanup
        my_log "TTS service response: $_info"

        json_init
        json_load "$_info"
        json_get_var _path path
        json_cleanup

        my_log "TTS audio file created: $_path"

        # If loop playback, cache the path
        [ "$loop_play" = "1" ] && CACHED_AUDIO_PATH="$_path"
    fi

    # Check if audio file exists
    if [ ! -f "$_path" ]; then
        my_log "Error: Audio file not found: $_path"
        return 1
    fi

    # Build playback parameters
    local player_args="-f $_path"

    # Play audio
    my_log "Starting audio playback: miplayer $player_args"
    miplayer $player_args
    local play_result=$?

    if [ $play_result -eq 0 ]; then
        my_log "Audio playback completed successfully"
    else
        my_log "Error: Audio playback failed with code $play_result"
    fi

    # Based on parameters, decide whether to delete the audio file
    if [ "$delete_audio" = "1" ]; then
        [ -f "$_path" ] && {
            rm "$_path"
            my_log "Deleted audio file: $_path"
        }
        # Clear cached path
        CACHED_AUDIO_PATH=""
    fi

    return $play_result
}

# Playback flow function to execute the playback process
play_flow() {
    local text="$1"
    local delete_audio="$2"
    local loop_play="$3"
    local notify_pns="$4"

    my_log "Starting play flow - text: '$text', delete_audio: $delete_audio, loop_play: $loop_play, notify_pns: $notify_pns"

    # Execute playback flow
    my_log "Getting current player state"
    player_stat=$(/usr/bin/mphelper mute_stat)
    my_log "Current player state: $player_stat"

    my_log "Pausing current playback"
    /usr/bin/mphelper pause

    notify_pns_vpm 14 "$notify_pns"

    my_log "Calling play_text function"
    play_text "$text" "$delete_audio" "$loop_play"
    local play_result=$?

    notify_pns_vpm 15 "$notify_pns"

    if [ "$player_stat" = "1" ]; then
        my_log "Restoring original playback state"
        /usr/bin/mphelper play
    else
        my_log "No need to restore playback (original state was not playing)"
    fi

    my_log "Play flow completed with result: $play_result"
    return $play_result
}

# Cleanup function to clean up cached audio files
cleanup_audio_cache() {
    if [ -n "$CACHED_AUDIO_PATH" ] && [ -f "$CACHED_AUDIO_PATH" ]; then
        rm "$CACHED_AUDIO_PATH"
        my_log "Cleaned up cached audio file: $CACHED_AUDIO_PATH"
    fi
    CACHED_AUDIO_PATH=""
}

# Signal handling function to perform cleanup on exit
cleanup_on_exit() {
    my_log "Script interrupted, performing cleanup..."
    cleanup_audio_cache
    exit 0
}

# Main program
main() {
    # Default parameters
    local loop_play=0
    local interval=1  # Default interval 1 second (used in loop)
    local keep_audio=0
    local text=""
    local force_delete_audio=0
    local notify_pns=0

    my_log "Script started with arguments: $@"

    # Parse command line arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            -l|--loop)
                loop_play=1
                my_log "Loop playback enabled"
                shift
                ;;
            -i|--interval)
                interval="$2"
                my_log "Interval set to: $interval seconds"
                shift 2
                ;;
            -k|--keep-audio)
                keep_audio=1
                my_log "Keep audio file enabled"
                shift
                ;;
            -d|--delete-audio)
                force_delete_audio=1
                my_log "Force delete audio file enabled"
                shift
                ;;
            -n|--notify-pns)
                notify_pns=1
                my_log "Event notifications pns(vpm) enabled"
                shift
                ;;
            -h|--help)
                usage
                ;;
            -*)
                my_log "Unknown option: $1"
                usage
                ;;
            *)
                text="$1"
                my_log "Text to speech: $text"
                shift
                ;;
        esac
    done

    # Parameter validation
    if [ -z "$text" ]; then
        my_log "Error: No text provided for TTS"
        usage
    fi

    # Validate interval format
    if ! echo "$interval" | grep -qE '^[0-9]+(\.[0-9]+)?$'; then
        my_log "Error: Invalid interval format: $interval"
        exit 1
    fi

    # Based on default configuration, determine deletion policy
    local delete_audio=1  # Default delete (non-loop playback)

    if [ "$loop_play" = "1" ]; then
        # Loop playback: default do not delete audio
        delete_audio=0
        my_log "Loop mode: audio file will be kept by default"

        # If force delete option is enabled, override default behavior
        if [ "$force_delete_audio" = "1" ]; then
            delete_audio=1
            my_log "Force delete enabled: audio file will be deleted even in loop mode"
        fi
    else
        # Non-loop playback: default delete, unless -k is specified
        if [ "$keep_audio" = "1" ]; then
            delete_audio=0
            my_log "Single mode with -k: audio file will be kept"
        else
            delete_audio=1
            my_log "Single mode: audio file will be deleted by default"
        fi

        # Force delete option takes highest priority in non-loop mode
        if [ "$force_delete_audio" = "1" ]; then
            delete_audio=1
            my_log "Force delete enabled: audio file will be deleted"
        fi
    fi

    my_log "Final parameters - Text: '$text', Loop: $loop_play, Interval: ${interval}s, Delete Audio: $delete_audio, Notify: $notify_pns"

    my_log "Playback parameters:"
    my_log "  Text: $text"
    my_log "  Loop: $([ "$loop_play" = "1" ] && echo "Yes" || echo "No")"
    if [ "$loop_play" = "1" ]; then
        my_log "  Interval: ${interval} seconds"
    fi
    my_log "  Delete Audio: $([ "$delete_audio" = "1" ] && echo "Yes" || echo "No")"
    my_log "  Notifications: $([ "$notify_pns" = "1" ] && echo "Enabled" || echo "Disabled")"
    if [ "$loop_play" = "1" ] && [ "$delete_audio" = "0" ]; then
        my_log "  Note: Audio file will be kept in loop mode for reuse"
    elif [ "$loop_play" != "1" ] && [ "$delete_audio" = "0" ]; then
        my_log "  Note: Audio file will be kept in single mode (using -k option)"
    elif [ "$force_delete_audio" = "1" ]; then
        my_log "  Note: Force delete enabled - audio file will be deleted after playback"
    fi
    my_log ""

    # Set up signal handling to ensure cleanup on exit
    trap cleanup_on_exit EXIT INT TERM

    if [ "$loop_play" = "1" ]; then
        # Loop playback mode
        my_log "Entering loop playback mode"
        my_log "Starting loop playback, press Ctrl+C to stop..."
        my_log "Default config: Interval ${interval} seconds, do not delete audio files"

        local loop_count=0
        while true; do
            loop_count=$((loop_count + 1))
            my_log "Starting playback loop #$loop_count"

            play_flow "$text" "$delete_audio" "$loop_play" "$notify_pns"

            my_log "Playback loop #$loop_count completed, waiting $interval seconds"
            sleep "$interval"
        done
    else
        # Single playback mode
        my_log "Entering single playback mode"
        if [ "$delete_audio" = "1" ]; then
            my_log "Single playback mode with default delete audio"
        else
            my_log "Single playback mode with keep audio option"
        fi

        play_flow "$text" "$delete_audio" "$loop_play" "$notify_pns"
        local play_result=$?

        # If not loop playback, clean up cache audio files
        cleanup_audio_cache

        my_log "Script finished with exit code: $play_result"
        exit $play_result
    fi
}

# Global variable to cache audio path for loop playback
CACHED_AUDIO_PATH=""

# Execute main program
main "$@"
