use neon::prelude::*;
use node::NodeManager;
use open_xiaoai::services::connect::{message::MessageManager, rpc::RPC};

use runtime::runtime;
use serde_json::json;
use server::AppServer;

mod node;
mod runtime;
mod server;

#[neon::export]
async fn start() -> () {
    let _ = AppServer::run().await;
}

#[neon::export]
async fn run_shell(script: String, timeout_millis: f64) -> String {
    let res = RPC::instance()
        .call_remote(
            "run_shell",
            Some(json!(script)),
            Some(timeout_millis as u64),
        )
        .await;
    match res {
        Err(e) => format!("run_shell error: {}", e),
        Ok(res) => serde_json::to_string(&res.data.unwrap()).unwrap(),
    }
}

#[neon::export]
async fn on_output_data(bytes: Vec<u8>) -> bool {
    MessageManager::instance()
        .send_stream("play", bytes, None)
        .await
        .is_ok()
}

#[neon::main]
fn main(mut cx: ModuleContext) -> NeonResult<()> {
    let _ = neon::set_global_executor(&mut cx, runtime());
    neon::registered().export(&mut cx)?;
    NodeManager::instance().init(cx);
    Ok(())
}
