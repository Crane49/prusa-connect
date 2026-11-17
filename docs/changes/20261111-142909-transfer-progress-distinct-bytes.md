### Fixed

- A file on its way to a Prusa printer no longer reads 100 % within a second and stays there for minutes. A Buddy board asks for the end of a G-code file first, and the progress counted how far into the file the bytes sent so far reached. It now counts the bytes of the file the printer has been sent, each once.
