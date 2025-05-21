use axum::{
    body::Body,
    middleware::Next,
    response::Response,
    http::Request,
};
use std::time::Instant;
use log::info;

pub async fn logging(
    req: Request<Body>,
    next: Next,
) -> Response {
    let start = Instant::now();
    let path = req.uri().path().to_owned();
    let method = req.method().clone();

    let response = next.run(req).await;

    let duration = start.elapsed();
    info!("{} {} completed in {:?}", method, path, duration);

    response
}