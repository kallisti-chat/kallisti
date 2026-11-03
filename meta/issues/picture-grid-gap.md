# Three pictures: a gap in the grid

## Summary

A message with three pictures shows the first one in a cell that spans two
rows (lib/fileGrid). With a portrait first picture, that cell grows to the
picture's own height and stretches both rows: the two square pictures on
the right sit at the top of their rows with a big gap between them (the
user's screenshot on test.kallisti.chat).

## Requirements

- The tall cell takes the height of the two rows beside it; its picture
  is cropped (object-fit cover), whatever its ratio.

## Acceptance Criteria

- Reproduced headless with a portrait first picture; after the fix the
  grid is two square rows high with no gap.
