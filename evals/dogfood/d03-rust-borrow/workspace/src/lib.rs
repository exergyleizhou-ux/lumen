/// Collect the longest word and its length.
/// BUG: borrow checker error — `longest` borrows `words` while it is moved.
pub fn longest_word(words: Vec<String>) -> (String, usize) {
    let mut longest = &words[0];
    for w in words {
        if w.len() > longest.len() {
            longest = &w;
        }
    }
    (longest.clone(), longest.len())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn picks_longest() {
        let words = vec!["a".to_string(), "abc".to_string(), "ab".to_string()];
        assert_eq!(longest_word(words), ("abc".to_string(), 3));
    }
}
