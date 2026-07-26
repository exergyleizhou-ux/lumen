#[derive(Debug, Clone, PartialEq, Default)]
pub struct Config {
    pub host: Option<String>,
    pub port: Option<u16>,
    pub verbose: Option<bool>,
}

impl Config {
    /// Overlay `other` on top of `self`: a field set in `other` wins, a field
    /// that is None in `other` must KEEP the value from `self`.
    /// BUG: None in `other` currently clobbers the existing value.
    pub fn merge(self, other: Config) -> Config {
        Config {
            host: other.host,
            port: other.port,
            verbose: other.verbose,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn none_does_not_clobber() {
        let base = Config { host: Some("a".into()), port: Some(80), verbose: Some(true) };
        let overlay = Config { host: None, port: Some(443), verbose: None };
        let merged = base.merge(overlay);
        assert_eq!(merged.host.as_deref(), Some("a"));
        assert_eq!(merged.port, Some(443));
        assert_eq!(merged.verbose, Some(true));
    }
}
