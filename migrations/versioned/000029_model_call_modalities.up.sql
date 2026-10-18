ALTER TABLE model_calls
 ADD COLUMN text_input_tokens bigint CHECK (text_input_tokens >= 0),
 ADD COLUMN image_input_tokens bigint CHECK (image_input_tokens >= 0),
 ADD COLUMN image_input_price numeric(24,10) CHECK (image_input_price >= 0);
