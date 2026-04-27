use crate::task::{Status, Task};
use ratatui::{
    style::Style,
    widgets::{Block, Borders, Paragraph},
    Frame,
};

pub struct TaskCard {
    task: Task,
    is_selected: bool,
}

impl TaskCard {
    pub fn new(task: Task, is_selected: bool) -> Self {
        TaskCard { task, is_selected }
    }

    pub fn render(&self, f: &mut Frame<'_>, area: ratatui::layout::Rect) {
        let truncated_title = if self.task.title.len() > 20 {
            format!("{}...", &self.task.title[..17])
        } else {
            self.task.title.clone()
        };

        let status_indicator = match self.task.status {
            Status::Open => ("[ ]", Style::default().fg(ratatui::style::Color::DarkGray)),
            Status::InProgress => ("[#]", Style::default().fg(ratatui::style::Color::Yellow)),
            Status::Blocked => ("[!]", Style::default().fg(ratatui::style::Color::Red)),
            Status::Done => ("[x]", Style::default().fg(ratatui::style::Color::Green)),
        };

        let block_style = if self.is_selected {
            Style::default()
                .bg(ratatui::style::Color::Blue)
                .fg(ratatui::style::Color::White)
        } else {
            Style::default()
        };

        let block = Block::default().borders(Borders::ALL).style(block_style);

        let content = format!(
            "{} {}\n  {}",
            status_indicator.0, truncated_title, self.task.branch
        );

        let paragraph = Paragraph::new(content)
            .style(Style::default().fg(ratatui::style::Color::White))
            .block(block);

        f.render_widget(paragraph, area);
    }
}
