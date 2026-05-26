#!/bin/sh
# Enable notify to pns(vpm) by default

# Log function to log messages
my_log() {
    echo "$(date '+%Y-%m-%d %H:%M:%S')[$0] - $1"
}

# Check if the TTS script exists and is executable
check_script() {
    SCRIPT_DIR=$(dirname "$(readlink -f "$0")")
    local script_path="${SCRIPT_DIR}/$1"
    local script_name="$1"

    if [ ! -f "$script_path" ]; then
        my_log "Error ${script_name} not found: $script_path"
        return 1
    fi

    return 0
}

# Main function to execute the TTS script with provided arguments
main() {
    local script_name="tts_play.sh"

    check_script $script_name || exit 1

    my_log "params: $@"
    $script_name "$@" -n
    local exit_code=$?

    case $exit_code in
        0) my_log "execute success" ;;
        *) my_log "execute failed, exit code: $exit_code" ;;
    esac

    return $exit_code
}

# Execute main function
main "$@"
