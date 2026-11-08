package parser

// lineIndexer incrementally indexes a contiguous source. A CR completes a
// physical line immediately; an LF at the start of the next chunk extends that
// ending instead of starting another line. No source text is retained here.
type lineIndexer struct {
	ends     []int
	offset   int
	maxLines int
	hasCR    bool
}

func (s *lineIndexer) feed(text string) error {
	if len(text) == 0 {
		return nil
	}
	base := s.offset
	s.offset += len(text)
	i := 0
	if s.hasCR {
		if text[0] == '\n' {
			s.ends[len(s.ends)-1]++
			i++
		}
		s.hasCR = false
	}
	if i < len(text) && len(s.ends) == s.maxLines {
		return s.limitError()
	}
	for ; i < len(text); i++ {
		if text[i] != '\n' && text[i] != '\r' {
			continue
		}
		if text[i] == '\r' {
			if i+1 == len(text) {
				s.hasCR = true
			} else if text[i+1] == '\n' {
				i++
			}
		}
		s.ends = append(s.ends, base+i+1)
		if i+1 < len(text) && len(s.ends) == s.maxLines {
			return s.limitError()
		}
	}
	return nil
}

func (s *lineIndexer) finish() []int {
	last := 0
	if len(s.ends) > 0 {
		last = s.ends[len(s.ends)-1]
	}
	if last < s.offset {
		s.ends = append(s.ends, s.offset)
	}
	return s.ends
}

func (s *lineIndexer) limitError() error {
	return &LimitError{Resource: "lines", Limit: s.maxLines, Observed: s.maxLines + 1}
}
