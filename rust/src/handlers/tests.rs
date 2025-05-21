#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::Request;
    use tower::ServiceExt;

    #[tokio::test]
    async fn test_query_handler() {
        let proxy = ChronoProxy::new();
        let app = Router::new()
            .route("/api/v1/query", get(super::query_handler))
            .with_state(proxy);

        let response = app
            .oneshot(
                Request::builder()
                    .uri("/api/v1/query?query=up")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
    }
}