use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use reqwest::Client;
use std::time::Duration;

#[derive(Clone)]
pub struct ChronoProxy {
    pub offsets: Vec<i64>,
    pub timeframes: Vec<String>,
}

impl ChronoProxy {
    pub fn new() -> Self {
        ChronoProxy {
            offsets: vec![
                0,
                7 * 24 * 3600,
                14 * 24 * 3600,
                21 * 24 * 3600,
                28 * 24 * 3600,
            ],
            timeframes: vec![
                "current".into(),
                "7days".into(),
                "14days".into(),
                "21days".into(),
                "28days".into(),
            ],
        }
    }

    pub fn timeframes(&self) -> Vec<String> {
        self.timeframes.clone()
    }
}

#[derive(Debug, Serialize, Deserialize)]
pub struct PrometheusResponse<T> {
    pub status: String,
    pub data: PrometheusData<T>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct PrometheusData<T> {
    #[serde(rename = "resultType")]
    pub result_type: String,
    pub result: Vec<T>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct InstantSeries {
    pub metric: HashMap<String, String>,
    pub value: (i64, String),
}

#[derive(Debug, Serialize, Deserialize)]
pub struct RangeSeries {
    pub metric: HashMap<String, String>,
    pub values: Vec<(i64, String)>,
}