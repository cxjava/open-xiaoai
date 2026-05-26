# 小爱音箱设备脚本速查

这一坨脚本是从小爱设备固件镜像 squashfs 里挑出来的——
位于 `tools/client-patch/temp/squashfs-root/{bin,usr/bin,usr/sbin}`，原盘几百个。

本目录只保留**对二次开发有用**的那一小撮：项目当前正在调用的、能教你设备
内部状态机怎么转的、以及一些可以直接拿来加新功能的工具。

> 源版本：L09A（小爱音箱），其它型号的脚本路径大致相同，名字偶尔差点。
> 这里的拷贝纯粹用于阅读和参考，**不要**改完之后 push 回设备覆盖原盘。

---

## 一、项目当前直接依赖的脚本

### `mphelper`（路径：`/usr/bin/mphelper`）

mediaplayer 的薄壳 wrapper，本项目里到处在调。封装的是 `ubus call mediaplayer`
那一坨。最常用：

| 子命令 | 实际 ubus 调用 | 说明 |
| --- | --- | --- |
| `play` | `player_play_operation {"action":"play","media":"common"}` | 恢复播放 |
| `pause` | `player_play_operation {"action":"pause","media":"common"}` | 暂停（项目里 `Player.Stop()` 用的就是这条） |
| `toggle` | `player_play_operation {"action":"toggle","media":"common"}` | 播放/暂停切换 |
| `next` / `prev` | `player_play_operation {"action":"next" / "prev",...}` | 切歌 |
| `iot_next` / `iot_prev` | 同上但不放提示音 | 适合自动化场景 |
| `mute_stat` | `player_get_play_status` 然后 JSON 解 `status` 字段 | 返回 `"1"` 表示在播 |
| `volume_get` | `player_get_context` 取 `volume` | 0-100 |
| `volume_set N` | `player_set_volume {"volume":N}` | 含 beep |
| `volume_up` / `volume_down` | 加减 `VOLUME_STEP`（部分型号 8，其它 4） | |
| `continuous_volume_set N` | `player_set_continuous_volume` | 连续滑动音量条用 |
| `tone URL` | `player_play_url {"url":...,"type":1}` | 单 URL 播放 |

注意 `next` / `prev` 这几个会顺手 `qplayer play key_prev_next.opus` 出"哒"声并
post 一条 `mibrain aivs_track_post`，自动化里大多想要的是 `iot_next` / `iot_prev`
那种安静版本。

### `tts_play.sh`（路径：`/usr/sbin/tts_play.sh`）

把一段文字转成本地 wav 后用 `miplayer` 播出来。本项目 `Player.Speak(text)` 走的
就是这条。流程：

```
mphelper pause                     # 把当前歌暂停
ubus call mibrain text_to_speech   # 拿到 TTS 音频文件路径
miplayer -f <wav>                  # 直接 ALSA 播
[restore]                          # 原来是 Playing 的话再 mphelper play
```

**坑**（在 `pkg/music/player.go` 注释里也有写）：

- 同步阻塞 3-5 秒（要等音频播完才返回）
- 中间会 `mphelper pause`，PlayingMonitor 会上报 `Paused → Idle → Playing`
  这段颠簸，绝对不能当成"歌曲播完"——项目里用 `suppressUntil` 窗口忽略这段时间
- 依赖 mibrain 服务；`AbortXiaoAI` 重启 `mico_aivs_lab` 时这条会暂时哑掉 1-2s
- 支持 `-l` 循环、`-k` 保留音频文件、`-d` 强制删、`-n` 通知 PNS

### `tts_play2.sh`（路径：`/usr/sbin/tts_play2.sh`）

`tts_play.sh -n` 的薄包装——默认开 PNS 事件通知。除此之外行为一致。

---

## 二、理解设备状态机必读

### `wakeup.sh`（路径：`/bin/wakeup.sh`）

唤醒/语音 pipeline 的总调度脚本，被 `mico_aivs_lab` 在各种事件下回调。**强烈
建议读完**——能看清楚小爱在每个状态下到底动了哪些 ubus、放了哪些提示音、点了
哪些 LED。

关键 case 摘要：

| `$1` | 做什么 |
| --- | --- |
| `WuW` / `WuW_first` / `WuW_oneshot` | 唤醒词命中。`player_wakeup action:start` 进入 wakeup 缓存态——**这个状态下 `player_play_url` 不会立即播，而是塞进 PlayList，等 `ready` 再 flush**。这条规则是 `pkg/music/player.go` 里 PlayURL 实现的核心约束。 |
| `WuW_uploading` | 把唤醒前后 1-2s 的多路音频（nuance/soundai/gmems/horizon/xiaomi）打包给 `pns_upload_helper`，用于云端模型训练 |
| `ready` / `ready_delay` | 云端 NLP 决策完，`player_wakeup action:stop` 退出 wakeup 缓存，刚才缓存的 URL 真正开播 |
| `stop` | 唤醒未匹配/超时，回到空闲态。包含 `shut_led 9` 关掉提示灯环 |
| `bf` / `bf_end` | beamforming 角度可视化，控制 LED 1 闪向声源方向 |
| `think` / `speek` / `asrstart` / `asrend` | LED 状态机：思考 / 播放 / 收音中 / 收音结束 |
| `wifi_disconnect` / `mibrain_*` | 各种错误音（`/usr/share/sound/*.opus`） |
| `welcome` / `welcome_sync` | 开机欢迎语 |
| `command_timeout` | "我没听清"提示 |

> 想做"完全屏蔽云端唤醒响应"，等于是要拦住 `wakeup.sh ready` 这个分支——
> 项目的 `AbortXiaoAI()` 是通过重启 `mico_aivs_lab` 一刀切，比 patch `wakeup.sh`
> 简单且 OTA 不需要 root。

---

## 三、提示音 / 直接出声的小工具

### `notify.sh`（路径：`/bin/notify.sh`）

非常薄，三种系统通知音：

```sh
notify.sh shutdown   # /usr/share/common_sound/shutdown.opus
notify.sh ble        # /usr/share/sound/ble.mp3
notify.sh setup      # /usr/share/sound/setupdone.mp3
```

适合脚本里加"操作成功/失败"的简短反馈。

### `easy_play`（路径：`/bin/easy_play`）

```sh
easy_play <sound_file> [volume]
```

直接走 `aplay` 播。可选第二个参数设音量（结束自动恢复）。这条**不经过
mediaplayer**，所以不会动 PlayList，也不会和当前歌曲打架。

### `tplay`（路径：`/bin/tplay`）

```sh
tplay <raw_pcm_file>     # 假定 16kHz mono S16_LE
```

最朴素的 raw PCM 播放，会先 `ubus call mediaplayer player_play_operation pause`
让出主声卡。给低延迟、纯本地 PCM 用。

---

## 四、系统辅助 / 调试

### `network_probe.sh`（路径：`/usr/sbin/network_probe.sh`）

由 crond 周期触发，写状态到 `/tmp/network_status`：

```
wireless=0;dns=0;internet=0
```

`0` 表示 OK、`1` 表示坏。逻辑：

1. `gateway_check` — ping 网关 + arping 兜底
2. `dns_check` — 对 `www.mi.com / baidu.com / taobao.com / qq.com` 做 nslookup
3. `internet_check` — 同样几个域名做 ping

`apps/client/boot.sh` 里"等网络就绪"那一段，思路就是这个。

### `getmac.sh`（路径：`/bin/getmac.sh`）

```sh
getmac.sh           # WiFi MAC
getmac.sh sn        # 序列号
getmac.sh did       # MIoT did + key
getmac.sh miio_did  # 只要 did
getmac.sh miio_key  # 只要 key
getmac.sh mac_bt    # 蓝牙 MAC
```

本质是把一堆 `micocfg_*` 二进制串起来。要给设备贴标签 / 上报 / 远程定位时方便。

### `reboot.sh`（路径：`/usr/bin/reboot.sh`）

优雅重启的标准动作：

```
mphelper pause               # 暂停播放
easy_logcut                  # 滚动一次日志
touch /data/status/upload_log
qplayer play shutdown.opus   # 关机音
killall -USR2 udhcpc         # 主动释放 DHCP 租约（USR2 是 udhcpc 的 release 信号）
reboot
```

需要重启设备时直接 `ssh root@音箱 /usr/bin/reboot.sh` 比裸 `reboot` 体验好很多。

### `silentboot.sh`（路径：`/bin/silentboot.sh`）

静默启动开关，写 u-boot 环境变量 + `/tmp/silent.flag`：

```sh
silentboot.sh set     # 下次启动不放欢迎语
silentboot.sh get     # 查询当前 flag (echo 0 或 1)
silentboot.sh clear   # 关闭
```

适合开发时频繁重启不想每次都听"小爱同学已连接 WiFi"。

---

## 五、`mediaplayer` ubus 接口完整列表

从设备 `/usr/bin/mediaplayer` 二进制 strings 提取，**没有官方文档**，参数靠
试和源码反推。常用的几条：

| Method | 用途 |
| --- | --- |
| `player_play_url` | 播单个 URL，`type:1` 表示交互式播放（最常用）|
| `player_play_album_playlist` | 推一整张专辑/播放列表（云端搜索结果走这个） |
| `player_play_resource` | 播某个 resource id |
| `player_play_music` | 直接放音乐（带元数据） |
| `player_play_index` | 跳到当前 list 的第 N 条 |
| `player_play_operation` | `{action: play/pause/toggle/next/prev/channel, media: common/wakeup_local/...}` |
| `player_play_private_fm` | 私人电台 |
| `player_play_alarm_reminder` | 闹钟/提醒 |
| `player_get_play_status` | 读 `status`（1=播放）|
| `player_get_context` | 读完整上下文，包括 `volume` |
| `player_get_latest_playlist` | 读当前 PlayList |
| `player_set_volume` | `{volume: 0-100, beep: 0/1}` |
| `player_set_continuous_volume` | 滑动条 |
| `player_set_loop` | 循环模式 |
| `player_set_positon` | 跳进度（注意拼写：`positon` 不是 `position`，固件 typo）|
| `player_set_shutdown_timer` | 定时关闭 |
| `player_wakeup` | `{action: start/stop/multistart}`——唤醒缓存态控制 |
| `player_mode` | 播放模式 |
| `player_reset` | **整张牌桌掀掉**：清 mibrain_list / track_list / 内部状态机 / clear tts file / reset last_dialog_id。`pkg/music/player.go` 的 `ResetMediaPlayer()` 走的是这条 |
| `player_aux` / `player_aux_operation` | 外接 AUX 输入 |
| `player_bt` / `player_airplay` / `player_dlna` / `player_usbaudio` | 各种外部源 |
| `player_miplay_cast` | 米家投屏 |

> `player_reset` 是排查"播完一首就被云端 PlayList 接管"那类问题的钥匙——
> 即便用 `AbortXiaoAI` 杀了云端 NLP，它**之前**已经 push 给 mediaplayer 的
> PlayList 还活着，得显式 reset 才能清干净。

## 六、几条最常用的命令速记

```sh
# 看当前播什么
ubus call mediaplayer player_get_play_status
ubus call mediaplayer player_get_context

# 强行停一切并清空云端残留 PlayList
ubus call mediaplayer player_reset

# 播本地 / HTTP URL
ubus call mediaplayer player_play_url '{"url":"http://10.0.0.1:8080/x.mp3","type":1}'

# 调音量
mphelper volume_set 30
mphelper volume_get

# 文字转语音
/usr/sbin/tts_play.sh '已连接'

# 杀掉小爱云端 NLP（项目里 AbortXiaoAI 走的就是这条）
/etc/init.d/mico_aivs_lab restart

# 系统提示音
notify.sh shutdown
qplayer play '{"play":"/usr/share/common_sound/welcome.opus"}'

# 看设备 SN / DID
getmac.sh sn
getmac.sh did
```
