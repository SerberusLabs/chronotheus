use axum::{
    extract::{Query, State},
    response::Json,
};
use serde_json::json;
use log::debug;
use crate::proxy::ChronoProxy;
use crate::utils::{
    fetch_windows_instant,
    build_last_month_average,
    extract_selectors,
    index_by_signature,
    append_with_command,
    append_compare,
    append_percent,
    filter_by_timeframe,
    dedupe_series,
};
use std::collections::HashMap;

#[derive(serde::Deserialize, Debug)]
pub struct QueryParams {
    query: String,
    #[serde(default)]
    time: Option<String>,
}

pub async fn query_handler(
    State(proxy): State<ChronoProxy>,
    Query(params): Query<QueryParams>,
) -> Json<serde_json::Value> {
    debug!("Processing query request with params: {:?}", params);

    let mut query_params = HashMap::new();
    query_params.insert("query".to_string(), vec![params.query]);
    if let Some(time) = params.time {
        query_params.insert("time".to_string(), vec![time]);
    }

    let (timeframe, command) = extract_selectors(&query_params["query"][0]);

    let current_series = match fetch_windows_instant(&proxy, &query_params).await {
        Ok(series) => series,
        Err(e) => {
            debug!("Error fetching series: {}", e);
            return Json(json!({
                "status": "error",
                "errorType": "execution",
                "error": e.to_string()
            }));
        }
    };

    let average_series = build_last_month_average(&current_series, false);
    
    // Create clones for index_by_signature
    let current_for_index = current_series.clone();
    let average_for_index = average_series.clone();
    
    let (current_map, avg_map) = index_by_signature(&current_for_index, &average_for_index);

    let mut final_result = current_series;
    final_result = append_with_command(final_result, average_series, &command);
    final_result = append_compare(final_result, &current_map, &avg_map, &command, false);
    final_result = append_percent(final_result, &current_map, &avg_map, &command, false);

    if !timeframe.is_empty() {
        final_result = filter_by_timeframe(final_result, &timeframe);
    }

    final_result = dedupe_series(final_result);

    Json(json!({
        "status": "success",
        "data": {
            "resultType": "vector",
            "result": final_result
        }
    }))
}