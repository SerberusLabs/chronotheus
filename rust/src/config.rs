use serde::Deserialize;
use std::path::PathBuf;

#[derive(Debug, Deserialize)]
pub struct Config {
    pub upstream_url: String,
    pub listen_addr: String,
    pub listen_port: u16,
    pub timeframes: Vec<String>,
    pub offsets: Vec<i64>,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            upstream_url: "http://localhost:9090".to_string(),
            listen_addr: "127.0.0.1".to_string(),
            listen_port: 8080,
            timeframes: vec![
                "current".to_string(),
                "7days".to_string(),
                "14days".to_string(),
                "21days".to_string(),
                "28days".to_string(),
            ],
            offsets: vec![
                0,
                7 * 24 * 3600,
                14 * 24 * 3600,
                21 * 24 * 3600,
                28 * 24 * 3600,
            ],
        }
    }
}

impl Config {
    pub fn load() -> Self {
        if let Ok(config_path) = std::env::var("CHRONOTHEUS_CONFIG") {
            let path = PathBuf::from(config_path);
            if path.exists() {
                if let Ok(contents) = std::fs::read_to_string(path) {
                    if let Ok(config) = toml::from_str(&contents) {
                        return config;
                    }
                }
            }
        }
        Self::default()
    }
}