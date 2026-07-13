from rkb.names import normalize

def test_normalize_handles_accents_and_filler():
    assert normalize("Nürburgring Nordschleife") == normalize("Nurburgring Nordschleife")
    assert normalize("Circuit de Barcelona-Catalunya GP Layout") == normalize("Circuit de Barcelona-Catalunya Grand Prix Layout")
    assert normalize("Autodrome Lago Maggiore - Centre") == normalize("Autodrome Lago Maggiore - Center")

def test_normalize_distinguishes_reverse():
    assert normalize("High Speed Ring") != normalize("High Speed Ring Reverse")
