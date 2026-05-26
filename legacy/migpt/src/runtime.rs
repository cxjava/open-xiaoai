use std::future::Future;

use once_cell::sync::OnceCell;
use tokio::runtime::Runtime;

static RUNTIME: OnceCell<Runtime> = OnceCell::new();

pub fn runtime() -> &'static Runtime {
    RUNTIME.get_or_try_init(Runtime::new).unwrap()
}

pub fn run_async<F>(future: F)
where
    F: Future + Send + 'static,
    F::Output: Send + 'static,
{
    runtime().spawn(future);
}
