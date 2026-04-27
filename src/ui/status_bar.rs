use ratatui::layout::Rect;
use ratatui::style::Style;
use ratatui::text::Text;
use ratatui::widgets::{Block, Paragraph};
use ratatui::Frame;

pub struct StatusBar {
    pub project_name: String,
    pub last_action: String,
}

impl StatusBar {
    pub fn new(project_name: String) -> Self {
        StatusBar {
            project_name,
            last_action: String::from("Ready"),
        }
    }

    pub fn set_message(&mut self, message: String) {
        self.last_action = message;
    }

    pub fn render(&self, f: &mut Frame<'_>, area: Rect) {
        let key_hints = " q:quit | n:new | j/k:move | Enter:select | Esc:cancel ";
        let content = Text::raw(format!(
            " Project: {} | {} | {} ",
            self.project_name, self.last_action, key_hints
        ));

        let widget = Paragraph::new(content)
            .style(
                Style::default()
                    .fg(ratatui::style::Color::White)
                    .bg(ratatui::style::Color::Blue),
            )
            .block(Block::default().borders(ratatui::widgets::Borders::ALL));

        f.render_widget(widget, area);
    }
}
