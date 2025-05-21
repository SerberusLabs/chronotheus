use std::collections::{HashMap, HashSet};
use regex::Regex;
use reqwest::Client;
use serde_json::Value;
use crate::models::Series;
use crate::proxy::ChronoProxy;

pub fn dedupe_series(series: Vec<Series>) -> Vec<Series> {
    let mut seen = HashSet::new();
    let mut result = Vec::new();

    for s in series {
        let key = serde_json::to_string(&s.metric).unwrap();
        if !seen.contains(&key) {
            seen.insert(key);
            result.push(s);
        }
    }
    result
}

pub fn extract_selectors(query: &str) -> (String, String) {
    let tf_re = Regex::new(r#"chrono_timeframe="([^"]+)""#).unwrap();
    let cmd_re = Regex::new(r#"_command="([^"]+)""#).unwrap();
    
    let timeframe = tf_re
        .captures(query)
        .and_then(|cap| cap.get(1))
        .map(|m| m.as_str().to_string())
        .unwrap_or_default();
    
    let command = cmd_re
        .captures(query)
        .and_then(|cap| cap.get(1))
        .map(|m| m.as_str().to_string())
        .unwrap_or_default();
    
    (timeframe, command)
}

pub fn is_raw_tf(tf: &str, timeframes: &[String]) -> bool {
    !tf.is_empty() && timeframes.contains(&tf.to_string())
}

pub fn build_last_month_average(series: &[Series], is_range: bool) -> Vec<Series> {
    let mut averages = Vec::new();
    
    for s in series {
        let mut avg_metric = s.metric.clone();
        avg_metric.insert("chrono_timeframe".to_string(), "lastMonthAverage".to_string());
        
        let avg_series = if is_range {
            let values = s.values.as_ref().unwrap();
            let avg_values: Vec<(i64, String)> = values.iter()
                .map(|(ts, val)| {
                    let num_val = val.parse::<f64>().unwrap_or(0.0);
                    (*ts, format!("{:.3}", num_val))
                })
                .collect();
            
            Series {
                metric: avg_metric,
                value: None,
                values: Some(avg_values),
            }
        } else {
            let val = s.value.as_ref().unwrap();
            Series {
                metric: avg_metric,
                value: Some((val.0, val.1.clone())),
                values: None,
            }
        };
        
        averages.push(avg_series);
    }
    
    averages
}

pub fn append_with_command(mut series: Vec<Series>, avg: Vec<Series>, command: &str) -> Vec<Series> {
    if command.is_empty() {
        series.extend(avg);
    }
    series
}

pub fn index_by_signature<'a>(
    current: &'a [Series],
    average: &'a [Series],
) -> (HashMap<String, &'a Series>, HashMap<String, &'a Series>) {
    let mut current_map = HashMap::new();
    let mut average_map = HashMap::new();

    for series in current {
        let mut sig = series.metric.clone();
        sig.remove("chrono_timeframe");
        let key = serde_json::to_string(&sig).unwrap();
        current_map.insert(key, series);
    }

    for series in average {
        let mut sig = series.metric.clone();
        sig.remove("chrono_timeframe");
        let key = serde_json::to_string(&sig).unwrap();
        average_map.insert(key, series);
    }

    (current_map, average_map)
}

pub fn append_compare(
    mut series: Vec<Series>,
    current_map: &HashMap<String, &Series>,
    avg_map: &HashMap<String, &Series>,
    command: &str,
    is_range: bool,
) -> Vec<Series> {
    if command != "compareAgainstLast28" {
        return series;
    }

    for (sig, cur) in current_map {
        if let Some(avg) = avg_map.get(sig) {
            let mut compare_metric = cur.metric.clone();
            compare_metric.insert(
                "chrono_timeframe".to_string(),
                "compareAgainstLast28".to_string(),
            );

            let compare_series = if is_range {
                let cur_values = cur.values.as_ref().unwrap();
                let avg_values = avg.values.as_ref().unwrap();
                
                let compare_values: Vec<(i64, String)> = cur_values
                    .iter()
                    .zip(avg_values.iter())
                    .map(|((ts, cur_val), (_, avg_val))| {
                        let cur_num = cur_val.parse::<f64>().unwrap_or(0.0);
                        let avg_num = avg_val.parse::<f64>().unwrap_or(0.0);
                        (*ts, format!("{:.3}", cur_num - avg_num))
                    })
                    .collect();

                Series {
                    metric: compare_metric,
                    value: None,
                    values: Some(compare_values),
                }
            } else {
                let (cur_ts, cur_val) = cur.value.as_ref().unwrap();
                let (_, avg_val) = avg.value.as_ref().unwrap();
                
                let cur_num = cur_val.parse::<f64>().unwrap_or(0.0);
                let avg_num = avg_val.parse::<f64>().unwrap_or(0.0);
                
                Series {
                    metric: compare_metric,
                    value: Some((*cur_ts, format!("{:.3}", cur_num - avg_num))),
                    values: None,
                }
            };

            series.push(compare_series);
        }
    }

    series
}

pub fn append_percent(
    mut series: Vec<Series>,
    current_map: &HashMap<String, &Series>,
    avg_map: &HashMap<String, &Series>,
    command: &str,
    is_range: bool,
) -> Vec<Series> {
    if command != "percentCompareAgainstLast28" {
        return series;
    }

    for (sig, cur) in current_map {
        if let Some(avg) = avg_map.get(sig) {
            let mut percent_metric = cur.metric.clone();
            percent_metric.insert(
                "chrono_timeframe".to_string(),
                "percentCompareAgainstLast28".to_string(),
            );

            let percent_series = if is_range {
                let cur_values = cur.values.as_ref().unwrap();
                let avg_values = avg.values.as_ref().unwrap();
                
                let percent_values: Vec<(i64, String)> = cur_values
                    .iter()
                    .zip(avg_values.iter())
                    .map(|((ts, cur_val), (_, avg_val))| {
                        let cur_num = cur_val.parse::<f64>().unwrap_or(0.0);
                        let avg_num = avg_val.parse::<f64>().unwrap_or(0.0);
                        let percent = if avg_num != 0.0 {
                            ((cur_num - avg_num) / avg_num) * 100.0
                        } else {
                            0.0
                        };
                        (*ts, format!("{:.3}", percent))
                    })
                    .collect();

                Series {
                    metric: percent_metric,
                    value: None,
                    values: Some(percent_values),
                }
            } else {
                let (cur_ts, cur_val) = cur.value.as_ref().unwrap();
                let (_, avg_val) = avg.value.as_ref().unwrap();
                
                let cur_num = cur_val.parse::<f64>().unwrap_or(0.0);
                let avg_num = avg_val.parse::<f64>().unwrap_or(0.0);
                let percent = if avg_num != 0.0 {
                    ((cur_num - avg_num) / avg_num) * 100.0
                } else {
                    0.0
                };
                
                Series {
                    metric: percent_metric,
                    value: Some((*cur_ts, format!("{:.3}", percent))),
                    values: None,
                }
            };

            series.push(percent_series);
        }
    }

    series
}

pub fn filter_by_timeframe(series: Vec<Series>, timeframe: &str) -> Vec<Series> {
    series
        .into_iter()
        .filter(|s| s.metric.get("chrono_timeframe").map_or(false, |tf| tf == timeframe))
        .collect()
}

pub async fn fetch_windows_instant(
    proxy: &ChronoProxy,
    params: &HashMap<String, Vec<String>>,
) -> anyhow::Result<Vec<Series>> {
    let client = Client::new();
    let mut all_series = Vec::new();

    for (offset, timeframe) in proxy.offsets.iter().zip(proxy.timeframes.iter()) {
        let mut window_params = params.clone();
        
        // Adjust time parameter if present
        if let Some(time) = window_params.get_mut("time") {
            if let Some(t) = time.first_mut() {
                if let Ok(timestamp) = t.parse::<i64>() {
                    *t = (timestamp + offset).to_string();
                }
            }
        }

        // Add timeframe label
        if let Some(query) = window_params.get_mut("query") {
            if let Some(q) = query.first_mut() {
                *q = format!(r#"{}{{chrono_timeframe="{}"}}"#, q, timeframe);
            }
        }

        let query_string = build_query_string(&window_params);
        let url = format!("http://localhost:9090/api/v1/query?{}", query_string);

        let response = client.get(&url).send().await?.json::<Value>().await?;
        
        if let Some(result) = response.get("data").and_then(|d| d.get("result")) {
            let series: Vec<Series> = serde_json::from_value(result.clone())?;
            all_series.extend(series);
        }
    }

    Ok(all_series)
}

pub async fn fetch_windows_range(
    proxy: &ChronoProxy,
    params: &HashMap<String, Vec<String>>,
) -> anyhow::Result<Vec<Series>> {
    let client = Client::new();
    let mut all_series = Vec::new();

    for (offset, timeframe) in proxy.offsets.iter().zip(proxy.timeframes.iter()) {
        let mut window_params = params.clone();
        
        // Adjust start/end parameters
        for param in ["start", "end"].iter() {
            if let Some(time) = window_params.get_mut(*param) {
                if let Some(t) = time.first_mut() {
                    if let Ok(timestamp) = t.parse::<i64>() {
                        *t = (timestamp + offset).to_string();
                    }
                }
            }
        }

        // Add timeframe label
        if let Some(query) = window_params.get_mut("query") {
            if let Some(q) = query.first_mut() {
                *q = format!(r#"{}{{chrono_timeframe="{}"}}"#, q, timeframe);
            }
        }

        let query_string = build_query_string(&window_params);
        let url = format!("http://localhost:9090/api/v1/query_range?{}", query_string);

        let response = client.get(&url).send().await?.json::<Value>().await?;
        
        if let Some(result) = response.get("data").and_then(|d| d.get("result")) {
            let series: Vec<Series> = serde_json::from_value(result.clone())?;
            all_series.extend(series);
        }
    }

    Ok(all_series)
}

pub fn parse_params(query_string: &str) -> HashMap<String, Vec<String>> {
    let mut params = HashMap::new();
    
    for pair in query_string.split('&').filter(|s| !s.is_empty()) {
        if let Some((key, value)) = pair.split_once('=') {
            let key = urlencoding::decode(key).unwrap_or_default().into_owned();
            let values = value
                .split(',')
                .map(|v| urlencoding::decode(v).unwrap_or_default().into_owned())
                .collect::<Vec<_>>();
            params.insert(key, values);
        }
    }
    
    params
}

pub fn strip_label(params: &mut HashMap<String, Vec<String>>, key: &str, label: &str) {
    if let Some(values) = params.get_mut(key) {
        for value in values {
            let pattern = format!(r#",?{}="[^"]*""#, label);
            let re = Regex::new(&pattern).unwrap();
            *value = re.replace_all(value, "").to_string();
            
            *value = value
                .replace(",,", ",")
                .replace("{,", "{")
                .replace(",}", "}");
        }
    }
}

pub fn build_query_string(params: &HashMap<String, Vec<String>>) -> String {
    params
        .iter()
        .flat_map(|(key, values)| {
            values.iter().map(move |value| {
                format!("{}={}", 
                    urlencoding::encode(key),
                    urlencoding::encode(value))
            })
        })
        .collect::<Vec<_>>()
        .join("&")
}