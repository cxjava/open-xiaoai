#!/bin/sh

case "$1" in
    sn)
        micocfg_sn
        ;;
    did)
        micocfg_miot_did
        micocfg_miot_key
        ;;
    miio_did)
        micocfg_miot_did
        ;;
    miio_key)
        micocfg_miot_key
        ;;
    mac_bt)
        micocfg_bt_mac
        ;;
    *)
        micocfg_mac
        ;;
esac
