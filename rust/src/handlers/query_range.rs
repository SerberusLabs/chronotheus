use axum::{
    extract::{Query, State},
    response::Json,
};
use serde_json::json;
use log::debug;
use crate::proxy::ChronoProxy;
use crate::utils::{
    fetch_windows_range,
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
pub struct QueryRangeParams {
    query: String,
    start: String,
    end: String,
    step: String,
}

pub async fn query_range_handler(
    State(proxy): State<ChronoProxy>,
    Query(params): Query<QueryRangeParams>,
) -> Json<serde_json::Value> {
    debug!("Processing query range request with params: {:?}", params);

    let mut query_params = HashMap::new();
    query_params.insert("query".to_string(), vec![params.query]);
    query_params.insert("start".to_string(), vec![params.start]);
    query_params.insert("end".to_string(), vec![params.end]);
    query_params.insert("step".to_string(), vec![params.step]);

    let (timeframe, command) = extract_selectors(&query_params["query"][0]);

    let current_series = match fetch_windows_range(&proxy, &query_params).await {
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

    let average_series = build_last_month_average(&current_series, true);
    
    // Create clones for index_by_signature
    let current_for_index = current_series.clone();
    let average_for_index = average_series.clone();
    
    let (current_map, avg_map) = index_by_signature(&current_for_index, &average_for_index);

    let mut final_result = current_series;
    final_result = append_with_command(final_result, average_series, &command);
    final_result = append_compare(final_result, &current_map, &avg_map, &command, true);
    final_result = append_percent(final_result, &current_map, &avg_map, &command, true);

    if !timeframe.is_empty() {
        final_result = filter_by_timeframe(final_result, &timeframe);
    }

    final_result = dedupe_series(final_result);

    Json(json!({
        "status": "success",
        "data": {
            "resultType": "matrix",
            "result": final_result
        }
    }))
}