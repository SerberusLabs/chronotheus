use axum::{
    response::{IntoResponse, Response},
    http::StatusCode,
    Json,
};
use serde_json::json;
use std::fmt;

#[derive(Debug)]
pub enum AppError {
    Upstream(reqwest::Error),
    Internal(String),
    InvalidTimeframe(String),
}

impl fmt::Display for AppError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            AppError::Upstream(e) => write!(f, "Upstream error: {}", e),
            AppError::Internal(e) => write!(f, "Internal error: {}", e),
            AppError::InvalidTimeframe(tf) => write!(f, "Invalid timeframe: {}", tf),
        }
    }
}

impl IntoResponse for AppError {
    fn into_response(self) -> Response {
        let (status, error_message) = match self {
            AppError::Upstream(err) => (
                StatusCode::BAD_GATEWAY,
                format!("Upstream error: {}", err),
            ),
            AppError::Internal(err) => (
                StatusCode::INTERNAL_SERVER_ERROR,
                format!("Internal error: {}", err),
            ),
            AppError::InvalidTimeframe(err) => (
                StatusCode::BAD_REQUEST,
                format!("Invalid timeframe: {}", err),
            ),
        };

        (status, Json(json!({
            "status": "error",
            "errorType": "execution",
            "error": error_message
        }))).into_response()
    }
}

impl From<reqwest::Error> for AppError {
    fn from(err: reqwest::Error) -> Self {
        Self::Upstream(err)
    }
}

impl From<anyhow::Error> for AppError {
    fn from(err: anyhow::Error) -> Self {
        Self::Internal(err.to_string())
    }
}