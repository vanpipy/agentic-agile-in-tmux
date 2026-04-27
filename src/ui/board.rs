use crate::config::BoardConfig;
use crate::task::Task;
use crate::ui::TaskCard;
use ratatui::{
    layout::{Constraint, Direction, Layout, Margin, Rect},
    style::Style,
    widgets::{Block, Borders, Paragraph, Scrollbar, ScrollbarOrientation, ScrollbarState},
    Frame,
};

pub struct BoardWidget {
    config: BoardConfig,
    selected_column: usize,
    selected_task: Option<usize>,
    scroll_offset: usize,
}

impl BoardWidget {
    pub fn new(config: BoardConfig) -> Self {
        BoardWidget {
            config,
            selected_column: 0,
            selected_task: None,
            scroll_offset: 0,
        }
    }

    pub fn selected_column(&self) -> usize {
        self.selected_column
    }

    #[allow(dead_code)]
    pub fn set_selected_column(&mut self, col: usize) {
        self.selected_column = col.min(self.config.columns.len().saturating_sub(1));
        self.selected_task = None;
        self.scroll_offset = 0;
    }

    pub fn selected_task(&self) -> Option<usize> {
        self.selected_task
    }

    #[allow(dead_code)]
    pub fn set_selected_task(&mut self, task: Option<usize>) {
        self.selected_task = task;
    }

    pub fn tasks_in_column<'a>(&self, tasks: &'a [Task], column_index: usize) -> Vec<&'a Task> {
        let col_name = self
            .config
            .columns
            .get(column_index)
            .map(|c| c.name.as_str())
            .unwrap_or("");
        tasks.iter().filter(|t| t.column == col_name).collect()
    }

    pub fn move_down(&mut self, tasks: &[Task]) {
        let col_tasks = self.tasks_in_column(tasks, self.selected_column);
        if let Some(sel) = self.selected_task {
            if sel < col_tasks.len().saturating_sub(1) {
                self.selected_task = Some(sel + 1);
            }
        } else if !col_tasks.is_empty() {
            self.selected_task = Some(0);
        }
    }

    pub fn move_up(&mut self) {
        if let Some(sel) = self.selected_task {
            if sel > 0 {
                self.selected_task = Some(sel - 1);
            }
        }
    }

    pub fn move_left(&mut self) {
        if self.selected_column > 0 {
            self.selected_column -= 1;
            self.selected_task = None;
            self.scroll_offset = 0;
        }
    }

    pub fn move_right(&mut self) {
        if self.selected_column < self.config.columns.len().saturating_sub(1) {
            self.selected_column += 1;
            self.selected_task = None;
            self.scroll_offset = 0;
        }
    }

    pub fn column_names(&self) -> Vec<String> {
        self.config.columns.iter().map(|c| c.name.clone()).collect()
    }

    pub fn render(&self, f: &mut Frame<'_>, area: Rect, tasks: &[Task]) {
        let column_areas = Layout::default()
            .direction(Direction::Horizontal)
            .constraints(
                self.config
                    .columns
                    .iter()
                    .map(|_| Constraint::Percentage(100 / self.config.columns.len() as u16))
                    .collect::<Vec<_>>(),
            )
            .split(area);

        for (i, column) in self.config.columns.iter().enumerate() {
            let column_area = column_areas[i];
            let col_tasks = self.tasks_in_column(tasks, i);

            let is_selected = i == self.selected_column;

            let column_block = Block::default()
                .title(format!(" {} ({}) ", column.name, col_tasks.len()))
                .borders(Borders::ALL)
                .style(if is_selected {
                    Style::default()
                        .bg(ratatui::style::Color::Blue)
                        .fg(ratatui::style::Color::White)
                } else {
                    Style::default()
                });

            f.render_widget(column_block, column_area);

            let inner_area = column_area.inner(Margin {
                horizontal: 1,
                vertical: 1,
            });

            if col_tasks.is_empty() {
                let empty_text = Paragraph::new("  (empty)")
                    .style(Style::default().fg(ratatui::style::Color::DarkGray));
                f.render_widget(empty_text, inner_area);
            } else {
                for (task_idx, task) in col_tasks.iter().enumerate() {
                    let is_task_selected = is_selected && self.selected_task == Some(task_idx);
                    let task_card = TaskCard::new((*task).clone(), is_task_selected);
                    let card_height = 3u16;
                    let card_area = Rect {
                        x: inner_area.x,
                        y: inner_area.y
                            + (task_idx as u16).saturating_sub(self.scroll_offset as u16),
                        width: inner_area.width,
                        height: card_height.min(inner_area.height.saturating_sub(task_idx as u16)),
                    };
                    if card_area.y < inner_area.y + inner_area.height
                        && card_area.y + card_area.height <= inner_area.y + inner_area.height
                    {
                        task_card.render(f, card_area);
                    }
                }
            }

            if col_tasks.len() > inner_area.height as usize / 3 {
                let scrollbar = Scrollbar::new(ScrollbarOrientation::VerticalRight)
                    .style(Style::default().fg(ratatui::style::Color::White));
                let mut scroll_state =
                    ScrollbarState::new(col_tasks.len().max(1) - inner_area.height as usize / 3);
                scroll_state = scroll_state.position(self.scroll_offset);
                f.render_stateful_widget(scrollbar, column_area, &mut scroll_state);
            }
        }
    }
}
