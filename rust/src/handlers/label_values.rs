use axum::{
    extract::{Path, State},
    response::Json,
};
use serde_json::json;
use log::debug;
use crate::proxy::ChronoProxy;
use reqwest::Client;

pub async fn label_values_handler(
    State(proxy): State<ChronoProxy>,
    Path(label): Path<String>,
) -> Json<serde_json::Value> {
    debug!("Processing label values request for label: {}", label);

    // Handle our custom timeframe label
    if label == "chrono_timeframe" {
        return Json(json!({
            "status": "success",
            "data": proxy.timeframes
        }));
    }

    let client = Client::new();
    let url = format!("http://localhost:9090/api/v1/label/{}/values", label);

    match client.get(&url).send().await {
        Ok(response) => {
            match response.json::<serde_json::Value>().await {
                Ok(data) => Json(data),
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