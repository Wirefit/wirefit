package io.wirefit.extract;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.fasterxml.jackson.annotation.JsonSubTypes;
import com.fasterxml.jackson.annotation.JsonTypeInfo;
import com.fasterxml.jackson.annotation.JsonTypeName;
import com.fasterxml.jackson.databind.BeanDescription;
import com.fasterxml.jackson.databind.JavaType;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.introspect.AnnotatedMember;
import com.fasterxml.jackson.databind.introspect.BeanPropertyDefinition;
import com.fasterxml.jackson.databind.node.ArrayNode;
import com.fasterxml.jackson.databind.node.ObjectNode;
import com.fasterxml.jackson.datatype.jdk8.Jdk8Module;

import java.lang.annotation.Annotation;
import java.lang.reflect.AnnotatedElement;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.Map;
import java.util.Optional;
import java.util.TreeMap;
import java.util.TreeSet;
import java.util.UUID;

/**
 * wirefit Java extractor (Phase 1, PRD 1.3).
 *
 * Emits wirefit IR for the closed type graph of the given DTO classes, using
 * Jackson's own introspection so naming strategies, {@code @JsonIgnore},
 * {@code @JsonProperty} and {@code @JsonInclude} behave exactly as the
 * service's serialization does.
 *
 * Usage: java -cp &lt;service classpath&gt;:&lt;jackson jars&gt; io.wirefit.extract.WirefitExtract FQN...
 * Output: one JSON object on stdout: { "&lt;fqn&gt;": &lt;IR schema&gt;, ... }
 *
 * Presence/nullability mapping (documented in extractors/java/README.md):
 *   primitive                          -> required, non-nullable
 *   Optional&lt;T&gt;                  -> optional, non-nullable
 *   @JsonInclude(NON_NULL/NON_ABSENT/NON_EMPTY) -> optional, non-nullable
 *   @JsonProperty(required=true)       -> required (overrides the above)
 *   any @Nonnull/@NotNull/@NonNull     -> non-nullable
 *   other reference types              -> required, nullable
 */
public final class WirefitExtract {

    public static void main(String[] args) throws Exception {
        if (args.length == 0) {
            System.err.println("usage: WirefitExtract <dto-class-fqn>...");
            System.exit(2);
        }
        ObjectMapper mapper;
        try {
            mapper = buildMapper();
        } catch (Unsupported e) {
            System.err.println("wirefit-extract: " + e.getMessage());
            System.exit(2);
            return;
        }
        WirefitExtract x = new WirefitExtract(mapper);
        ObjectNode out = mapper.createObjectNode();
        try {
            for (String fqn : args) {
                Class<?> cls = Class.forName(fqn);
                out.set(fqn, x.schemaFor(mapper.constructType(cls), new ArrayDeque<>(), fqn));
            }
        } catch (ClassNotFoundException e) {
            System.err.println("wirefit-extract: class not found on classpath: " + e.getMessage());
            System.exit(2);
        } catch (Unsupported e) {
            System.err.println("wirefit-extract: " + e.getMessage());
            System.exit(2);
        }
        System.out.println(mapper.writerWithDefaultPrettyPrinter().writeValueAsString(out));
    }

    static final class Unsupported extends RuntimeException {
        Unsupported(String msg) { super(msg); }
    }

    /**
     * Builds the ObjectMapper used for introspection. With
     * {@code -Dwirefit.mapper=com.acme.JacksonConfig#objectMapper} (manifest:
     * {@code settings.java-mapper}) the service's own statically-provided
     * mapper is used, so custom modules and naming strategies are honored —
     * the documented fallback for Spring ObjectMapper discovery (PRD §8).
     */
    private static ObjectMapper buildMapper() {
        String hint = System.getProperty("wirefit.mapper", "");
        ObjectMapper m;
        if (hint.isEmpty()) {
            m = new ObjectMapper();
        } else {
            String[] parts = hint.split("#");
            if (parts.length != 2) {
                throw new Unsupported("wirefit.mapper must be <class-fqn>#<static-method>, got: " + hint);
            }
            try {
                Object o = Class.forName(parts[0]).getMethod(parts[1]).invoke(null);
                m = (ObjectMapper) o;
            } catch (ReflectiveOperationException | ClassCastException e) {
                throw new Unsupported("wirefit.mapper " + hint + " failed: " + e);
            }
        }
        // Duplicate module registrations are ignored by Jackson, so this is
        // safe when the provided mapper already registered Jdk8Module.
        return m.registerModule(new Jdk8Module());
    }

    private final ObjectMapper mapper;

    private WirefitExtract(ObjectMapper mapper) { this.mapper = mapper; }

    private static final Map<Class<?>, String> SCALARS = new TreeMap<>(
            (a, b) -> a.getName().compareTo(b.getName()));
    static {
        SCALARS.put(String.class, "string");
        SCALARS.put(char.class, "string");
        SCALARS.put(Character.class, "string");
        SCALARS.put(UUID.class, "uuid");
        SCALARS.put(int.class, "int32");
        SCALARS.put(Integer.class, "int32");
        SCALARS.put(short.class, "int32");
        SCALARS.put(Short.class, "int32");
        SCALARS.put(long.class, "int64");
        SCALARS.put(Long.class, "int64");
        SCALARS.put(float.class, "float32");
        SCALARS.put(Float.class, "float32");
        SCALARS.put(double.class, "float64");
        SCALARS.put(Double.class, "float64");
        SCALARS.put(BigDecimal.class, "decimal");
        SCALARS.put(BigInteger.class, "decimal");
        SCALARS.put(boolean.class, "bool");
        SCALARS.put(Boolean.class, "bool");
        SCALARS.put(java.time.LocalDate.class, "date");
        SCALARS.put(java.time.Instant.class, "datetime");
        SCALARS.put(java.time.OffsetDateTime.class, "datetime");
        SCALARS.put(java.time.ZonedDateTime.class, "datetime");
        SCALARS.put(java.time.LocalDateTime.class, "datetime");
        SCALARS.put(java.util.Date.class, "datetime");
        SCALARS.put(java.time.Duration.class, "duration");
    }

    private ObjectNode schemaFor(JavaType t, Deque<Class<?>> stack, String ctx) {
        ObjectNode n = mapper.createObjectNode();
        Class<?> raw = t.getRawClass();

        // byte[] is the bytes scalar, not an array.
        if (t.isArrayType() && t.getContentType().getRawClass() == byte.class) {
            n.put("type", "string").put("x-ct-scalar", "bytes");
            return n;
        }
        String scalar = SCALARS.get(raw);
        if (scalar != null) {
            n.put("type", jsonType(scalar)).put("x-ct-scalar", scalar);
            return n;
        }
        if (raw.isEnum()) {
            n.put("type", "string").put("x-ct-scalar", "string");
            ArrayNode vals = n.putArray("enum");
            TreeSet<String> names = new TreeSet<>();
            for (Object c : raw.getEnumConstants()) {
                names.add(((Enum<?>) c).name());
            }
            for (String name : names) vals.add(name);
            return n;
        }
        if (t.isCollectionLikeType() || t.isArrayType()) {
            n.put("type", "array");
            n.set("items", schemaFor(t.getContentType(), stack, ctx + "[]"));
            return n;
        }
        if (t.isMapLikeType()) {
            if (t.getKeyType() != null && t.getKeyType().getRawClass() != String.class) {
                throw new Unsupported("non-string map key at " + ctx + ": " + t);
            }
            // Open maps carry their value type (SPEC open question 2, resolved).
            n.put("type", "object");
            n.set("additionalProperties", schemaFor(t.getContentType(), stack, ctx + "{}"));
            return n;
        }
        if (raw == Object.class || raw.getName().startsWith("com.fasterxml.jackson.databind.JsonNode")) {
            throw new Unsupported("untyped value (Object/JsonNode) at " + ctx
                    + " — give the field a concrete DTO type or exclude it");
        }
        if (raw == Optional.class) {
            // Nested Optional inside containers: unwrap; optionality only has
            // meaning at the property level where it is handled by the caller.
            return schemaFor(t.containedType(0), stack, ctx);
        }

        // Polymorphism: tagged unions only (SPEC open question 1).
        JsonTypeInfo ti = raw.getAnnotation(JsonTypeInfo.class);
        if (ti != null) {
            return unionFor(raw, ti, stack, ctx);
        }

        // Plain bean.
        if (stack.contains(raw)) {
            n.put("x-ct-recursive", true);
            return n;
        }
        stack.push(raw);
        try {
            return beanFor(t, stack, ctx);
        } finally {
            stack.pop();
        }
    }

    private ObjectNode unionFor(Class<?> raw, JsonTypeInfo ti, Deque<Class<?>> stack, String ctx) {
        if (ti.use() != JsonTypeInfo.Id.NAME) {
            throw new Unsupported("only @JsonTypeInfo(use=NAME) unions are supported, at " + ctx);
        }
        if (ti.include() != JsonTypeInfo.As.PROPERTY) {
            throw new Unsupported("only @JsonTypeInfo(include=PROPERTY) unions are supported, at " + ctx);
        }
        JsonSubTypes st = raw.getAnnotation(JsonSubTypes.class);
        if (st == null || st.value().length == 0) {
            throw new Unsupported("@JsonTypeInfo without closed @JsonSubTypes at " + ctx
                    + " — open inheritance is not checkable");
        }
        ObjectNode n = mapper.createObjectNode();
        String prop = ti.property().isEmpty() ? ti.use().getDefaultPropertyName() : ti.property();
        n.put("x-ct-discriminator", prop);
        ArrayNode oneOf = n.putArray("oneOf");
        TreeMap<String, Class<?>> branches = new TreeMap<>();
        for (JsonSubTypes.Type sub : st.value()) {
            String name = sub.name();
            if (name.isEmpty()) {
                JsonTypeName tn = sub.value().getAnnotation(JsonTypeName.class);
                name = (tn != null && !tn.value().isEmpty()) ? tn.value() : sub.value().getSimpleName();
            }
            branches.put(name, sub.value());
        }
        for (Map.Entry<String, Class<?>> e : branches.entrySet()) {
            ObjectNode branch = schemaFor(mapper.constructType(e.getValue()), stack,
                    ctx + "<" + e.getKey() + ">");
            branch.put("x-ct-discriminator-value", e.getKey());
            oneOf.add(branch);
        }
        return n;
    }

    private ObjectNode beanFor(JavaType t, Deque<Class<?>> stack, String ctx) {
        ObjectNode n = mapper.createObjectNode();
        n.put("type", "object");
        ObjectNode props = mapper.createObjectNode();
        TreeSet<String> required = new TreeSet<>();
        BeanDescription bd = mapper.getSerializationConfig().introspect(t);
        JsonInclude classInclude = t.getRawClass().getAnnotation(JsonInclude.class);

        for (BeanPropertyDefinition prop : bd.findProperties()) {
            AnnotatedMember member = prop.getPrimaryMember();
            if (member == null) continue;
            String name = prop.getName();
            JavaType pt = member.getType();

            boolean optional = false;
            boolean nullable = false;

            if (pt.getRawClass() == Optional.class) {
                optional = true;
                pt = pt.containedType(0);
            }
            if (!pt.isPrimitive() && !optional) {
                nullable = true; // reference type: Java may serialize null
            }
            if (suppressesNull(prop, classInclude)) {
                optional = true;  // null is omitted, never emitted
                nullable = false;
            }
            if (hasAnnotation(member, "Nonnull", "NotNull", "NonNull")) {
                nullable = false;
            }
            boolean explicitlyRequired = prop.getMetadata() != null
                    && Boolean.TRUE.equals(prop.getMetadata().getRequired());
            if (explicitlyRequired) {
                optional = false;
            }

            ObjectNode ps = schemaFor(pt, stack, ctx + "." + name);
            if (nullable) ps.put("x-ct-nullable", true);
            props.set(name, ps);
            if (!optional) required.add(name);
        }
        if (props.size() == 0) {
            throw new Unsupported("bean with no serializable properties at " + ctx
                    + " (" + t.getRawClass().getName() + ")");
        }
        n.set("properties", props);
        if (!required.isEmpty()) {
            ArrayNode req = n.putArray("required");
            for (String r : required) req.add(r);
        }
        return n;
    }

    private static boolean suppressesNull(BeanPropertyDefinition prop, JsonInclude classInclude) {
        JsonInclude.Include inc = null;
        JsonInclude.Value v = prop.findInclusion();
        if (v != null && v.getValueInclusion() != JsonInclude.Include.USE_DEFAULTS) {
            inc = v.getValueInclusion();
        } else if (classInclude != null) {
            inc = classInclude.value();
        }
        return inc == JsonInclude.Include.NON_NULL
                || inc == JsonInclude.Include.NON_ABSENT
                || inc == JsonInclude.Include.NON_EMPTY;
    }

    /** Nullability annotations matched by simple name: jakarta, javax, JetBrains, Lombok, Checker. */
    private static boolean hasAnnotation(AnnotatedMember member, String... simpleNames) {
        AnnotatedElement el = (AnnotatedElement) member.getMember();
        for (Annotation a : el.getAnnotations()) {
            String simple = a.annotationType().getSimpleName();
            for (String want : simpleNames) {
                if (simple.equals(want)) return true;
            }
        }
        return false;
    }

    private static String jsonType(String scalar) {
        switch (scalar) {
            case "bool": return "boolean";
            case "int32": case "int64": return "integer";
            case "float32": case "float64": case "decimal": return "number";
            default: return "string";
        }
    }
}
