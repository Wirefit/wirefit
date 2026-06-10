package conformance;

public class NullableEnum {
    public Status status;   // plain reference: present, may be null

    public enum Status { AMBER, GREEN }
}
