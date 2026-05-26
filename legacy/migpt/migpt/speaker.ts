import { jsonEncode } from "@mi-gpt/utils/parse";
import { RustServer } from "./open-xiaoai.js";
import type { ISpeaker } from "@mi-gpt/engine/base";

export interface CommandResult {
  stdout: string;
  stderr: string;
  exit_code: number;
}

class SpeakerManager implements ISpeaker {
  status: "playing" | "paused" | "idle" = "idle";

  /**
   * 获取播放状态
   */
  async getPlaying(sync = false) {
    if (sync) {
      // 同步远端最新状态
      const res = await this.runShell("mphelper mute_stat");
      if (res?.stdout.includes("1")) {
        this.status = "playing";
      } else if (res?.stdout.includes("2")) {
        this.status = "paused";
      }
    }
    return this.status;
  }

  /**
   * 播放/暂停
   */
  async setPlaying(playing = true) {
    const res = await this.runShell(
      playing ? "mphelper play" : "mphelper pause"
    );
    return res?.stdout.includes('"code": 0');
  }

  /**
   * 播放文字、音频链接、音频流
   */
  async play({
    text,
    url,
    bytes,
    timeout = 10 * 60 * 1000,
    blocking = false,
  }: {
    text?: string;
    url?: string;
    bytes?: Uint8Array;
    /**
     * 超时时长（毫秒）
     *
     * 默认 10 分钟
     */
    timeout?: number;
    /**
     * 是否阻塞运行(仅对播放文字、音频链接有效)
     *
     * 如果是则等到音频播放完毕才会返回
     */
    blocking?: boolean;
  }) {
    if (bytes) {
      return RustServer.on_output_data(bytes) as Promise<boolean>;
    }

    if (blocking) {
      const res = await this.runShell(
        url
          ? `miplayer -f '${url}'`
          : `/usr/sbin/tts_play.sh '${text || "你好"}'`,
        { timeout }
      );
      return res?.exit_code === 0;
    }

    const res = await this.runShell(
      url
        ? `ubus call mediaplayer player_play_url '${jsonEncode({
            url: url,
            type: 1,
          })}'`
        : `ubus call mibrain text_to_speech '${jsonEncode({
            text: text || "你好",
            save: 0,
          })}'`,
      { timeout }
    );
    return res?.stdout.includes('"code": 0') ?? false;
  }

  /**
   * （取消）唤醒小爱
   */
  async wakeUp(
    awake = true,
    options?: {
      /**
       * 静默唤醒
       */
      silent: boolean;
    }
  ) {
    const { silent = true } = options ?? {};
    const command = awake
      ? silent
        ? `ubus call pnshelper event_notify '{"src":1,"event":0}'`
        : `ubus call pnshelper event_notify '{"src":0,"event":0}'`
      : `
        ubus call pnshelper event_notify '{"src":3, "event":7}'
        sleep 0.1
        ubus call pnshelper event_notify '{"src":3, "event":8}'
    `;
    const res = await this.runShell(command);
    return res?.stdout.includes('"code": 0');
  }

  /**
   * 把文字指令交给原来的小爱执行
   */
  async askXiaoAI(
    text: string,
    options?: {
      /**
       * 静默执行
       */
      silent: boolean;
    }
  ) {
    const { silent = false } = options ?? {};
    const res = await this.runShell(
      `ubus call mibrain ai_service '${jsonEncode({
        tts: silent ? undefined : 1,
        nlp: 1,
        nlp_text: text,
      })}'`
    );
    return res?.stdout.includes('"code": 0');
  }

  /**
   * 中断原来小爱的运行
   *
   * 注意：重启需要大约 1-2s 的时间，在此期间无法使用小爱音箱自带的 TTS 服务
   */
  async abortXiaoAI() {
    const res = await this.runShell(
      "/etc/init.d/mico_aivs_lab restart >/dev/null 2>&1"
    );
    return res?.exit_code === 0;
  }

  /**
   * 获取启动分区
   */
  async getBoot() {
    const res = await this.runShell("echo $(fw_env -g boot_part)");
    return res?.stdout.trim();
  }

  /**
   * 设置启动分区
   */
  async setBoot(boot_part: "boot0" | "boot1") {
    const res = await this.runShell(
      `fw_env -s boot_part ${boot_part} >/dev/null 2>&1 && echo $(fw_env -g boot_part)`
    );
    return res?.stdout.includes(boot_part);
  }

  /**
   * 获取设备型号、序列号信息
   */
  async getDevice() {
    const res = await this.runShell("echo $(micocfg_model) $(micocfg_sn)");
    const info = res?.stdout.trim().split(" ");
    return {
      model: info?.[0] ?? "unknown",
      sn: info?.[1] ?? "unknown",
    };
  }

  /**
   * 获取麦克风状态
   */
  async getMic() {
    const res = await this.runShell(
      "[ ! -f /tmp/mipns/mute ] && echo on || echo off"
    );
    let status: "on" | "off" = "off";
    if (res?.stdout.includes("on")) {
      status = "on";
    }
    return status;
  }

  /**
   * 打开/关闭麦克风
   */
  async setMic(on = true) {
    const res = await this.runShell(
      on
        ? `ubus -t1 -S call pnshelper event_notify '{"src":3, "event":7}' 2>&1`
        : `ubus -t1 -S call pnshelper event_notify '{"src":3, "event":8}' 2>&1`
    );
    return res?.stdout.includes('"code":0');
  }

  /**
   * 执行脚本
   */
  async runShell(
    script: string,
    options?: {
      /**
       * 超时时间（单位：毫秒）
       */
      timeout?: number;
    }
  ): Promise<CommandResult | undefined> {
    const { timeout = 10 * 1000 } = options ?? {};
    try {
      const res = await RustServer.run_shell(script, timeout);
      if (res) {
        return JSON.parse(res);
      }
    } catch (_) {
      return undefined;
    }
  }
}

export const OpenXiaoAISpeaker = new SpeakerManager();
