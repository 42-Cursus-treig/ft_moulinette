#include <unistd.h>

int	ft_str_is_numeric(char *str);

int	main(int argc, char **argv)
{
	char	c;

	if (argc < 2)
		return (1);
	c = '0' + ft_str_is_numeric(argv[1]);
	write(1, &c, 1);
	return (0);
}
