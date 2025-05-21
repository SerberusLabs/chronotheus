use axum::{
    extract::State,
    response::Json,
};
use serde_json::json;
use log::debug;
use crate::proxy::ChronoProxy;
use reqwest::Client;
use anyhow::Result;

pub async fn labels_handler(
    State(_proxy): State<ChronoProxy>,
) -> Json<serde_json::Value> {
    debug!("Processing labels request");

    let client = Client::new();
    let url = "http://localhost:9090/api/v1/labels";

    match client.get(url).send().await {
        Ok(response) => {
            match response.json::<serde_json::Value>().await {
                Ok(mut data) => {
                    // Add our custom timeframe label
                    if let Some(labels) = data.get_mut("data").and_then(|d| d.as_array_mut()) {
                        labels.push(json!("chrono_timeframe"));
                    }
                    Json(data)
                }
                Err(e) => Json(json!({
                    "status": "error",
                    "errorType": "execution",
                    "error": e.to_string()
                }))
            }
        }
        Err(e) => Json(json!({
            "status": "error",
            "errorType": "execution",
            "error": e.to_string()
        }))
    }
}