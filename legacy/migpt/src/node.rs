use neon::prelude::*;
use std::sync::{Arc, LazyLock, Mutex};
use tokio::sync::oneshot;

use crate::runtime::run_async;

pub struct NodeManager {
    channel: Arc<Mutex<Option<Channel>>>,
}

static INSTANCE: LazyLock<NodeManager> = LazyLock::new(NodeManager::new);

impl NodeManager {
    pub fn new() -> Self {
        Self {
            channel: Arc::new(Mutex::new(None)),
        }
    }

    pub fn instance() -> &'static Self {
        &INSTANCE
    }

    pub fn init(&self, mut cx: ModuleContext) {
        let channel = cx.channel();
        *self.channel.lock().unwrap() = Some(channel);
    }

    pub async fn call_fn<R, F, F2>(&self, key: &str, map_arg: F, map_res: F2) -> Result<R, String>
    where
        R: Send + 'static,
        F: for<'a> Fn(&mut TaskContext<'a>) -> Handle<'a, JsValue> + Send + 'static,
        F2: Fn(&mut TaskContext<'_>, Handle<'_, JsValue>) -> Result<R, String>
            + Send
            + Sync
            + 'static,
    {
        let channel = match self.channel.lock().unwrap().as_ref() {
            Some(channel) => channel.clone(),
            None => return Err("NodeManager 尚未初始化".into()),
        };

        let key = key.to_string();
        let (tx, rx) = oneshot::channel::<Result<R, String>>();

        channel.send(move |mut cx| {
            let Ok(callbacks) = cx.global::<JsObject>("RUST_CALLBACKS") else {
                let _ = tx.send(Err("无法获取 RUST_CALLBACKS 对象".into()));
                return Ok(());
            };

            let Ok(callback) = callbacks.get::<JsFunction, _, _>(&mut cx, key.as_str()) else {
                let _ = tx.send(Err("找不到函数".into()));
                return Ok(());
            };

            let arg = map_arg(&mut cx);
            let this = cx.undefined();

            let Ok(res) = callback.call(&mut cx, this, [arg]) else {
                let _ = tx.send(Err("函数调用失败".into()));
                return Ok(());
            };

            if res.is_a::<JsPromise, _>(&mut cx) {
                let future = res
                    .downcast::<JsPromise, _>(&mut cx)
                    .unwrap()
                    .to_future(&mut cx, move |mut cx, res| match res {
                        Ok(res) => Ok(map_res(&mut cx, res)),
                        Err(err) => Ok(map_res(&mut cx, err)),
                    })
                    .unwrap();

                run_async(async move {
                    let res = future.await.unwrap();
                    let _ = tx.send(res);
                });
            } else {
                let _ = tx.send(map_res(&mut cx, res));
            }

            Ok(())
        });

        let res = rx.await;

        match res {
            Ok(res) => res,
            Err(_) => Err("接收数据失败".into()),
        }
    }
}
