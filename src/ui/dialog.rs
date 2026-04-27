use ratatui::{
    layout::{Margin, Rect},
    style::Style,
    text::Text,
    widgets::{Block, Borders, Clear, Paragraph},
    Frame,
};

pub struct CreateDialog {
    pub title: String,
    pub branch: String,
    pub column_index: usize,
    pub focus_index: usize,
}

impl CreateDialog {
    pub fn new() -> Self {
        CreateDialog {
            title: String::new(),
            branch: String::new(),
            column_index: 0,
            focus_index: 0,
        }
    }

    pub fn render(&self, f: &mut Frame<'_>, area: Rect, columns: &[String]) {
        let dialog_area = Self::centered_rect(area, 40, 12);
        f.render_widget(Clear, area);
        f.render_widget(
            Block::default()
                .title(" Create Task ")
                .borders(Borders::ALL)
                .style(Style::default().bg(ratatui::style::Color::DarkGray)),
            dialog_area,
        );

        let inner = dialog_area.inner(Margin {
            horizontal: 1,
            vertical: 1,
        });

        let title_display = format!(
            "Title: {}",
            if self.title.is_empty() {
                "_"
            } else {
                &self.title
            }
        );
        let branch_display = format!(
            "Branch: {}",
            if self.branch.is_empty() {
                "_"
            } else {
                &self.branch
            }
        );
        let column_display = format!(
            "Column: {}",
            columns
                .get(self.column_index)
                .cloned()
                .unwrap_or_else(|| "To Do".to_string())
        );

        let hints = if self.focus_index == 2 {
            " Tab: switch | Enter: confirm | Esc: cancel "
        } else {
            " Tab: switch | Esc: cancel "
        };

        let content = Text::raw(format!(
            "{}\n{}\n{}\n\n{}\n{}",
            title_display,
            branch_display,
            column_display,
            if self.focus_index == 0 {
                "  >>>"
            } else {
                "     "
            },
            if self.focus_index == 1 {
                "  >>>"
            } else {
                "     "
            },
        ));

        f.render_widget(Paragraph::new(content), inner);

        let hint_area = Rect {
            x: dialog_area.x + 1,
            y: dialog_area.y + dialog_area.height.saturating_sub(2),
            width: dialog_area.width.saturating_sub(2),
            height: 1,
        };
        f.render_widget(Paragraph::new(hints), hint_area);
    }

    fn centered_rect(area: Rect, width: u16, height: u16) -> Rect {
        Rect {
            x: area.x + (area.width.saturating_sub(width)) / 2,
            y: area.y + (area.height.saturating_sub(height)) / 2,
            width,
            height,
        }
    }

    pub fn handle_key(
        &mut self,
        key: ratatui::crossterm::event::KeyEvent,
    ) -> Option<DialogCommand> {
        use ratatui::crossterm::event::KeyCode;
        match key.code {
            KeyCode::Tab => {
                self.focus_index = (self.focus_index + 1) % 3;
                None
            }
            KeyCode::Enter => {
                if self.focus_index < 2 {
                    self.focus_index = 2;
                    None
                } else {
                    Some(DialogCommand::Confirm)
                }
            }
            KeyCode::Esc => Some(DialogCommand::Cancel),
            KeyCode::Left => {
                if self.focus_index == 2 {
                    self.column_index = self.column_index.saturating_sub(1);
                }
                None
            }
            KeyCode::Right => {
                if self.focus_index == 2 {
                    self.column_index += 1;
                }
                None
            }
            KeyCode::Char(c) => {
                if self.focus_index == 0 {
                    if self.title.len() < 100 {
                        self.title.push(c);
                    }
                } else if self.focus_index == 1 {
                    if self.branch.len() < 50 {
                        self.branch.push(c);
                    }
                }
                None
            }
            KeyCode::Backspace => {
                if self.focus_index == 0 {
                    self.title.pop();
                } else if self.focus_index == 1 {
                    self.branch.pop();
                }
                None
            }
            _ => None,
        }
    }
}

pub struct EditDialog {
    pub field_index: usize,
    pub value: String,
    pub focus_index: usize,
}

impl EditDialog {
    pub fn new() -> Self {
        EditDialog {
            field_index: 0,
            value: String::new(),
            focus_index: 0,
        }
    }

    pub fn render(&self, f: &mut Frame<'_>, area: Rect) {
        let dialog_area = Self::centered_rect(area, 40, 8);
        f.render_widget(Clear, area);
        f.render_widget(
            Block::default()
                .title(" Edit Task ")
                .borders(Borders::ALL)
                .style(Style::default().bg(ratatui::style::Color::DarkGray)),
            dialog_area,
        );

        let inner = dialog_area.inner(Margin {
            horizontal: 1,
            vertical: 1,
        });

        let fields = ["title", "branch", "status"];
        let field_display = format!("Field: {}", fields[self.field_index]);
        let value_display = format!(
            "Value: {}",
            if self.value.is_empty() {
                "_"
            } else {
                &self.value
            }
        );

        let content = Text::raw(format!(
            "{}\n{}\n\n{}",
            field_display,
            value_display,
            if self.focus_index == 1 {
                "  >>>"
            } else {
                "     "
            }
        ));

        f.render_widget(Paragraph::new(content), inner);
    }

    fn centered_rect(area: Rect, width: u16, height: u16) -> Rect {
        Rect {
            x: area.x + (area.width.saturating_sub(width)) / 2,
            y: area.y + (area.height.saturating_sub(height)) / 2,
            width,
            height,
        }
    }

    pub fn handle_key(
        &mut self,
        key: ratatui::crossterm::event::KeyEvent,
    ) -> Option<DialogCommand> {
        use ratatui::crossterm::event::KeyCode;
        match key.code {
            KeyCode::Tab => {
                self.focus_index = (self.focus_index + 1) % 2;
                if self.focus_index == 0 {
                    self.field_index = (self.field_index + 1) % 3;
                }
                None
            }
            KeyCode::Enter => {
                if self.focus_index == 0 {
                    self.focus_index = 1;
                    None
                } else {
                    Some(DialogCommand::Confirm)
                }
            }
            KeyCode::Esc => Some(DialogCommand::Cancel),
            KeyCode::Char(c) => {
                if self.focus_index == 1 && self.value.len() < 100 {
                    self.value.push(c);
                }
                None
            }
            KeyCode::Backspace => {
                if self.focus_index == 1 {
                    self.value.pop();
                }
                None
            }
            _ => None,
        }
    }
}

#[derive(Debug, Clone, Copy)]
pub enum DialogCommand {
    Confirm,
    Cancel,
}
