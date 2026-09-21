-- Sample problems and test cases for local development and integration tests.
-- Load after running goose migrations.

WITH two_sum AS (
    INSERT INTO problems (slug, title, description, difficulty)
    VALUES (
        'two-sum',
        'Two Sum',
        'Given an array of integers nums and an integer target, return the indices of the two numbers that add up to target.\n\nRead space-separated integers from stdin, then the target on its own line. Print the two indices (0-based) separated by a space.',
        'easy'
    )
    RETURNING id
)
INSERT INTO test_cases (problem_id, name, input, expected_output, is_hidden, display_order)
SELECT id, 'Sample 1', '2 7 11 15\n9', '0 1', false, 0 FROM two_sum
UNION ALL
SELECT id, 'Sample 2', '3 2 4\n6', '1 2', false, 1 FROM two_sum
UNION ALL
SELECT id, 'Hidden 1', '3 3\n6', '0 1', true, 10 FROM two_sum;

WITH reverse_string AS (
    INSERT INTO problems (slug, title, description, difficulty)
    VALUES (
        'reverse-string',
        'Reverse String',
        'Read a single string from stdin and print it reversed.',
        'easy'
    )
    RETURNING id
)
INSERT INTO test_cases (problem_id, name, input, expected_output, is_hidden, display_order)
SELECT id, 'Sample 1', 'hello', 'olleh', false, 0 FROM reverse_string
UNION ALL
SELECT id, 'Hidden 1', 'world', 'dlrow', true, 10 FROM reverse_string;
