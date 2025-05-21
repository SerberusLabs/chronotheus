use serde::{Serialize, Deserialize};
use std::collections::HashMap;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Series {
    pub metric: HashMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<(i64, String)>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub values: Option<Vec<(i64, String)>>,
}