use ratatui::crossterm::event::{Event, KeyCode, KeyEventKind};
use ratatui::prelude::*;
use ratatui::Terminal;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let backend = CrosstermBackend::new(std::io::stdout());
    let mut terminal = Terminal::new(backend)?;

    terminal.clear()?;
    terminal.draw(|frame| {
        let area = frame.area();
        let text = Text::raw("Hello Kanban");
        frame.render_widget(text, area);
    })?;

    loop {
        if let Event::Key(key) = ratatui::crossterm::event::read()? {
            if key.kind == KeyEventKind::Press && key.code == KeyCode::Char('q') {
                break;
            }
        }
    }

    Ok(())
}
