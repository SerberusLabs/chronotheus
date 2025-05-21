use axum::{
    Router,
    routing::{get, post},
    middleware::from_fn,
};
use clap::Parser;
use log::{info, LevelFilter};
use std::net::SocketAddr;

mod handlers;
mod proxy;
mod utils;
mod models;
mod middleware;

#[derive(Parser, Debug)]
#[clap(about = "Chronotheus - A Prometheus Historical Data Proxy")]
struct Args {
    #[clap(short, long, default_value = "false")]
    debug: bool,

    #[clap(short, long, default_value = "8080")]
    port: u16,
}

#[tokio::main]
async fn main() {
    let args = Args::parse();
    
    env_logger::builder()
        .filter_level(if args.debug { LevelFilter::Debug } else { LevelFilter::Info })
        .init();

    let proxy = proxy::ChronoProxy::new();

    let app = Router::new()
        .route("/api/v1/query", get(handlers::query_handler).post(handlers::query_handler))
        .route("/api/v1/query_range", get(handlers::query_range_handler))
        .route("/api/v1/labels", get(handlers::labels_handler))
        .route("/api/v1/label/:label/values", get(handlers::label_values_handler))
        .layer(from_fn(crate::middleware::logging))
        .with_state(proxy);

    let addr = SocketAddr::from(([127, 0, 0, 1], args.port));
    info!("🚀 Chronotheus proxy listening on {}", addr);

    axum::serve(tokio::net::TcpListener::bind(addr).await.unwrap(), app).await.unwrap();
}