#!/bin/sh
#


silent_flag_set() {
	/usr/bin/fw_env -s silent_boot 1 > /dev/null 2>&1
	echo "1" > /tmp/silent.flag
}

silent_flag_get() {
	val=`/usr/bin/fw_env -g silent_boot`
	if [ "$val" = "1" ]; then
		echo "1"
	else
		echo "0"
	fi
}

silent_flag_clear() {
	/usr/bin/fw_env -s silent_boot 0 > /dev/null 2>&1
	echo "0" > /tmp/silent.flag
}


case "$1" in
	"set")
		silent_flag_set
		;;
	"get")
		silent_flag_get
		;;
	"clear")
		silent_flag_clear
		;;
	"*")
		;;
esac
